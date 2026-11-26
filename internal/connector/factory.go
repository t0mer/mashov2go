package connector

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Build creates a connector of the given type from its stored (decrypted) config.
// hc may be nil. The caller must Close the result.
func Build(typ string, config []byte, hc *http.Client) (Connector, error) {
	switch typ {
	case "greenapi":
		var cfg GreenAPIConfig
		if err := json.Unmarshal(config, &cfg); err != nil {
			return nil, fmt.Errorf("green-api config: %w", err)
		}
		return NewGreenAPI(cfg, hc)
	case "telegram":
		var cfg struct {
			BotToken string `json:"bot_token"`
		}
		if err := json.Unmarshal(config, &cfg); err != nil {
			return nil, fmt.Errorf("telegram config: %w", err)
		}
		return NewTelegram(cfg.BotToken, "", hc)
	default:
		return nil, fmt.Errorf("unknown connector type %q", typ)
	}
}
