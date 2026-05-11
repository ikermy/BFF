package gintransport

import (
	"net/http"

	"github.com/ikermy/BFF/internal/ports"

	"github.com/gin-gonic/gin"
)

// respondRevisionList — общий хелпер для GET /revisions в /api/v1 и /internal.
// Возвращает только enabled-ревизии в формате {revisions: [{name, displayName, enabled}]}.
func respondRevisionList(c *gin.Context, store ports.RevisionConfigStore) {
	configs, err := store.ListConfigs(c.Request.Context())
	if err != nil {
		RespondError(c, err)
		return
	}

	type revisionItem struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Enabled     bool   `json:"enabled"`
	}
	items := make([]revisionItem, 0, len(configs))
	for _, cfg := range configs {
		if cfg.Enabled {
			items = append(items, revisionItem{
				Name:        cfg.Name,
				DisplayName: cfg.DisplayName,
				Enabled:     cfg.Enabled,
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"revisions": items})
}
