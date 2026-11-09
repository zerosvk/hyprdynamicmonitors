package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/fiffeek/hyprdynamicmonitors/internal/hyprconf"
)

type HyprPreviewPane struct {
	monitors []*MonitorSpec
	cfg      *config.Config
	clamp    bool
	width    int
	colors   *ColorsManager
}

func NewHyprPreviewPane(monitors []*MonitorSpec, cfg *config.Config, colors *ColorsManager) *HyprPreviewPane {
	return &HyprPreviewPane{
		monitors: monitors,
		cfg:      cfg,
		width:    0,
		clamp:    true,
		colors:   colors,
	}
}

func (h *HyprPreviewPane) format() hyprconf.Format {
	if h.cfg == nil {
		return hyprconf.DefaultFormat
	}
	return h.cfg.Get().General.ConfigFormat()
}

func (h *HyprPreviewPane) SetWidth(width int) {
	h.width = width
}

func (h *HyprPreviewPane) SetClamp(clamp bool) {
	h.clamp = clamp
}

func (h *HyprPreviewPane) View() string {
	var sections []string

	if len(h.monitors) == 0 {
		sections = append(sections, h.colors.TitleStyle().Render(
			"Hyprland Config Preview\n\nNo monitors to configure"))
	} else {
		sections = append(sections, h.colors.TitleStyle().Render("Hyprland Config Preview"))
	}

	format := h.format()
	for _, monitor := range h.monitors {
		hyprCommand := monitor.ToHyprConfig(format)
		if format == hyprconf.FormatHyprlang {
			hyprCommand = "monitor = " + monitor.ToHypr()
		}
		if h.clamp {
			hyprCommand = ClampTextTo(hyprCommand, h.width)
		}
		sections = append(sections, h.colors.MutedStyle().Render(hyprCommand))
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func ClampTextTo(text string, maxLen int) string {
	if maxLen < 0 {
		return text
	}
	if len(text) <= maxLen {
		return text
	}
	dots := "..."
	lenWithoutDots := maxLen - len(dots)
	return dots + text[len(text)-lenWithoutDots:]
}
