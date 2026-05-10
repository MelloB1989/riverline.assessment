package eval

import (
	"github.com/MelloB1989/karma/ai"
	"riverline_server/constants"
	"sync"
	"time"
)

const (
	judgeCallTimeout          = 90 * time.Second
	judgeCallTimeoutSlow      = 240 * time.Second
	internalGenerationTimeout = 60 * time.Second
	aiCallMaxAttempts         = 3
	judgeJSONParseMaxAttempts = 4
)

var (
	nvidiaNIMRequestsPerMinute = constants.AppCfg.Get().NvidiaNIMRPM
	unavailableJudgeModels     sync.Map
	providerRateLimitUntil     sync.Map
)

func init() {
	if nvidiaNIMRequestsPerMinute > 0 {
		ai.SetGlobalRateLimit(ai.NvidiaNIM, nvidiaNIMRequestsPerMinute, ai.RateLimitBehaviorWait)
	}
}
