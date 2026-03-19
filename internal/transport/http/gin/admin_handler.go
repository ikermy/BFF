package gintransport

import (
	"net/http"

	"github.com/ikermy/BFF/internal/domain"
	"github.com/ikermy/BFF/internal/ports"

	"github.com/gin-gonic/gin"
)

// AdminHandler — обработчики /admin/* (п.13 ТЗ).
type AdminHandler struct {
	topUpBonus  ports.TopUpBonusStore
	kafkaTopics ports.KafkaTopicStore
	timeouts    ports.TimeoutStore
	revisions   ports.RevisionConfigStore
}

func NewAdminHandler(
	topupBonus ports.TopUpBonusStore,
	kafkaTopics ports.KafkaTopicStore,
	timeouts ports.TimeoutStore,
	revisions ports.RevisionConfigStore,
) *AdminHandler {
	return &AdminHandler{
		topUpBonus:  topupBonus,
		kafkaTopics: kafkaTopics,
		timeouts:    timeouts,
		revisions:   revisions,
	}
}

// handleUpdateJSON — helper that binds JSON into v, runs optional validate and apply closures,
// and sends HTTP response or validation errors. This removes duplication across admin endpoints.
func (h *AdminHandler) handleUpdateJSON(c *gin.Context, v any, validate func() error, apply func() error) {
	if err := c.ShouldBindJSON(v); err != nil {
		RespondError(c, domain.NewValidationError(err.Error()))
		return
	}
	if validate != nil {
		if err := validate(); err != nil {
			RespondError(c, err)
			return
		}
	}
	if apply != nil {
		if err := apply(); err != nil {
			RespondError(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, v)
}

// ListRevisions — GET /admin/revisions (п.13.1 ТЗ).
func (h *AdminHandler) ListRevisions(c *gin.Context) {
	configs, err := h.revisions.ListConfigs(c.Request.Context())
	if err != nil {
		RespondError(c, err)
		return
	}

	// GET возвращает calculationChain как []string (имена полей) по формату ТЗ п.13.1
	type revisionView struct {
		Name             string   `json:"name"`
		DisplayName      string   `json:"displayName"`
		Enabled          bool     `json:"enabled"`
		CalculationChain []string `json:"calculationChain"`
	}
	views := make([]revisionView, 0, len(configs))
	for _, cfg := range configs {
		chain := make([]string, 0, len(cfg.CalculationChain))
		for _, entry := range cfg.CalculationChain {
			chain = append(chain, entry.Field)
		}
		views = append(views, revisionView{
			Name:             cfg.Name,
			DisplayName:      cfg.DisplayName,
			Enabled:          cfg.Enabled,
			CalculationChain: chain,
		})
	}
	c.JSON(http.StatusOK, gin.H{"revisions": views})
}

// UpdateRevision — PUT /admin/revisions/:revision (п.13.1 ТЗ).
func (h *AdminHandler) UpdateRevision(c *gin.Context) {
	revision := c.Param("revision")
	if revision == "" {
		RespondError(c, domain.NewValidationError("revision param is required"))
		return
	}

	var req domain.UpdateRevisionRequest
	h.handleUpdateJSON(c, &req, func() error {
		return validateUpdateRevision(req)
	}, func() error {
		return h.revisions.UpdateConfig(c.Request.Context(), revision, req)
	})
}

// validateUpdateRevision — проверяет тело PUT /admin/revisions/:revision.
func validateUpdateRevision(req domain.UpdateRevisionRequest) error {
	validSources := map[string]bool{"calculate": true, "user": true, "random": true}
	for _, entry := range req.CalculationChain {
		if entry.Field == "" {
			return domain.NewValidationError("calculationChain entry must have a non-empty field")
		}
		if !validSources[entry.Source] {
			return domain.NewValidationError("calculationChain source must be one of: calculate, user, random")
		}
	}
	return nil
}

// UpdateTopUpBonus — PUT /admin/config/topup-bonus (п.14.7 ТЗ).
func (h *AdminHandler) UpdateTopUpBonus(c *gin.Context) {
	var cfg domain.TopupBonusConfig
	h.handleUpdateJSON(c, &cfg, func() error {
		return validateTopupBonusConfig(cfg)
	}, func() error {
		return h.topUpBonus.Set(c.Request.Context(), cfg)
	})
}

// ListKafkaTopics — GET /admin/kafka/topics (п.13.3 ТЗ).
func (h *AdminHandler) ListKafkaTopics(c *gin.Context) {
	topics, err := h.kafkaTopics.List(c.Request.Context())
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"topics": topics})
}

// UpdateTimeouts — PUT /admin/config/timeouts (п.13.2 ТЗ).
// Принимает значения в миллисекундах, как в ТЗ и ENV-переменных (п.17.1).
func (h *AdminHandler) UpdateTimeouts(c *gin.Context) {
	var t domain.ServiceTimeouts
	h.handleUpdateJSON(c, &t, func() error {
		return validateTimeouts(t)
	}, func() error {
		return h.timeouts.Set(c.Request.Context(), t)
	})
}

// validateTimeouts — проверяет корректность значений таймаутов.
func validateTimeouts(t domain.ServiceTimeouts) error {
	if t.BarcodeGen <= 0 {
		return domain.NewValidationError("barcodeGen timeout must be > 0")
	}
	if t.Billing <= 0 {
		return domain.NewValidationError("billing timeout must be > 0")
	}
	if t.AI <= 0 {
		return domain.NewValidationError("ai timeout must be > 0")
	}
	// History и Auth опциональны: 0 означает «использовать дефолт клиента».
	if t.History < 0 {
		return domain.NewValidationError("history timeout must be >= 0")
	}
	if t.Auth < 0 {
		return domain.NewValidationError("auth timeout must be >= 0")
	}
	// Верхняя граница: не более 5 минут (защита от случайных значений)
	const maxMs = 5 * 60 * 1000
	if t.BarcodeGen > maxMs || t.Billing > maxMs || t.AI > maxMs {
		return domain.NewValidationError("timeout values must not exceed 300000ms (5 minutes)")
	}
	if t.History > maxMs || t.Auth > maxMs {
		return domain.NewValidationError("timeout values must not exceed 300000ms (5 minutes)")
	}
	return nil
}

// validateTopupBonusConfig — проверяет корректность уровней бонуса.
func validateTopupBonusConfig(cfg domain.TopupBonusConfig) error {
	for i, tier := range cfg.Tiers {
		if tier.MinAmount < 0 {
			return domain.NewValidationError("tier minAmount must be >= 0")
		}
		if tier.MaxAmount != nil && *tier.MaxAmount <= tier.MinAmount {
			return domain.NewValidationError("tier maxAmount must be greater than minAmount")
		}
		if tier.BonusPercent <= 0 || tier.BonusPercent > 100 {
			return domain.NewValidationError("tier bonusPercent must be between 0 and 100")
		}
		if i > 0 {
			prev := cfg.Tiers[i-1]
			if prev.MaxAmount != nil && tier.MinAmount < *prev.MaxAmount {
				return domain.NewValidationError("tiers must not overlap")
			}
		}
	}
	return nil
}
