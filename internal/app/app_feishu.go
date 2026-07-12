//go:build windows

package app

import (
	"context"
	"fmt"
	"log"

	"everevo/internal/a2a"
	"everevo/internal/config"
	"everevo/internal/feishu"
)

// ─── Feishu Bot API ─────────────────────────────────────────────

// GetFeishuConfig returns the Feishu bot configuration.
func (a *App) GetFeishuConfig() config.FeishuConfig {
	if a.cfg == nil {
		return config.FeishuConfig{}
	}
	return a.cfg.LLM.FeishuConfig
}

// UpdateFeishuConfig saves the Feishu config and rebuilds/restarts the bot.
func (a *App) UpdateFeishuConfig(cfg config.FeishuConfig) error {
	if a.cfg == nil {
		return fmt.Errorf("配置未初始化")
	}
	a.cfg.LLM.FeishuConfig = cfg
	if err := config.Save(a.cfg); err != nil {
		return fmt.Errorf("保存飞书配置失败: %w", err)
	}

	wasRunning := a.feishuClient != nil
	if wasRunning {
		a.feishuClient.Stop()
	}
	a.feishuClient = nil
	if cfg.AppID != "" && cfg.AppSecret != "" {
		a.feishuClient = feishu.NewClient(toFeishuConfig(cfg), a.handleFeishuMessage)
		if cfg.Enabled {
			go func() {
				if err := a.feishuClient.Start(a.ctx); err != nil {
					log.Printf("[feishu] start failed: %v", err)
				}
			}()
		}
	}
	a.emitChanged("feishu:changed", "update", "config")
	return nil
}

// StartFeishu connects the Feishu bot.
func (a *App) StartFeishu() error {
	cfg := a.cfg.LLM.FeishuConfig
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return fmt.Errorf("缺少 App ID / App Secret")
	}
	if a.feishuClient == nil {
		a.feishuClient = feishu.NewClient(toFeishuConfig(cfg), a.handleFeishuMessage)
	}
	if a.feishuClient.Status().Running {
		return nil
	}
	if err := a.feishuClient.Start(a.ctx); err != nil {
		return err
	}
	log.Println("[feishu] bot started")
	return nil
}

// StopFeishu disconnects the Feishu bot.
func (a *App) StopFeishu() error {
	if a.feishuClient != nil {
		a.feishuClient.Stop()
	}
	return nil
}

// GetFeishuStatus returns the Feishu bot connection status.
func (a *App) GetFeishuStatus() feishu.Status {
	if a.feishuClient == nil {
		return feishu.Status{}
	}
	return a.feishuClient.Status()
}

// ─── Feishu Bot internal ────────────────────────────────────────

// initFeishuClient wires up the Feishu bot at startup and auto-starts if enabled.
func (a *App) initFeishuClient() {
	cfg := a.cfg.LLM.FeishuConfig
	if cfg.AppID == "" || cfg.AppSecret == "" {
		return
	}
	a.feishuClient = feishu.NewClient(toFeishuConfig(cfg), a.handleFeishuMessage)
	if cfg.Enabled {
		go func() {
			if err := a.feishuClient.Start(a.ctx); err != nil {
				log.Printf("[feishu] auto-start failed: %v", err)
			}
		}()
	}
}

// handleFeishuMessage runs an incoming Feishu message through the A2A LLM path
// (reuses executeA2ATask for consistent system-prompt framing).
func (a *App) handleFeishuMessage(ctx context.Context, chatID, text string) (string, error) {
	_ = chatID
	return a.executeA2ATask(ctx, []a2a.Message{a2a.TextMessage("user", text)})
}

// toFeishuConfig maps the persisted config to the feishu package Config.
func toFeishuConfig(cfg config.FeishuConfig) feishu.Config {
	return feishu.Config{
		Enabled:           cfg.Enabled,
		AppID:             cfg.AppID,
		AppSecret:         cfg.AppSecret,
		VerificationToken: cfg.VerificationToken,
	}
}
