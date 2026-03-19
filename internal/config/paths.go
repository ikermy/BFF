package config

// Фиксированные project paths для конфигов, изменяемых через Admin API.
// Это не ENV-переменные и не часть п.17.1 ТЗ.
const (
	RevisionsDirPath     = "configs/revisions"
	TimeoutsConfigPath   = "configs/admin/timeouts.yaml"
	TopupBonusConfigPath = "configs/admin/topup-bonus.yaml"
)
