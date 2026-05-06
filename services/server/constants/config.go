package constants

import (
	"riverline_server/internal/models"

	"github.com/MelloB1989/karma/config"
)

var AppCfg = config.NewAppConfig[*models.AppConfig]().
	MustLoad().
	MustValidate()
