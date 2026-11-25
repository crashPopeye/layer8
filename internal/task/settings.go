package task

import (
	"fmt"
	"slices"

	"github.com/crashPopeye/layer8/internal/config"
)

// Themes are the UI palettes; the first is the default.
var Themes = []string{"rose-pine", "dash"}

// Theme is the UI palette saved in config.json.
func (s *Service) Theme() string {
	cfg, _ := config.Load(s.home)
	if !slices.Contains(Themes, cfg.Theme) {
		return Themes[0]
	}
	return cfg.Theme
}

// SetTheme saves the UI palette to config.json.
func (s *Service) SetTheme(theme string) error {
	if !slices.Contains(Themes, theme) {
		return fmt.Errorf("%w: unknown theme %q", ErrInvalid, theme)
	}
	cfg, err := config.Load(s.home)
	if err != nil {
		return err
	}
	cfg.Theme = theme
	return config.Save(s.home, cfg)
}
