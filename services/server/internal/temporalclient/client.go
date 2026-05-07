package temporalclient

import (
	"riverline_server/constants"

	"go.temporal.io/sdk/client"
)

func Dial() (client.Client, error) {
	return client.Dial(client.Options{
		HostPort: constants.AppCfg.Get().TemporalHostPort,
	})
}
