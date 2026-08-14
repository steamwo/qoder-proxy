//go:build desktop

package main

import (
	"fmt"

	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/steamwo/qoder-proxy/internal/desktop"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

var inlineContextMenu = struct {
	openID   string
	triggers map[string]*widget.Clickable
	choices  map[string]*widget.Clickable
}{
	triggers: make(map[string]*widget.Clickable),
	choices:  make(map[string]*widget.Clickable),
}

func cloneModelContextDefaults(source map[string]int) map[string]int {
	cloned := make(map[string]int, len(source))
	for modelID, tokens := range source {
		cloned[modelID] = tokens
	}
	return cloned
}

func (s *appState) modelContextDefault(model qoder.Model) int {
	s.mu.RLock()
	selected := s.settings.ModelContextDefaults[model.UpstreamID]
	s.mu.RUnlock()
	if selected <= 0 {
		return 0
	}
	if normalized, err := model.NormalizeContextWindow(selected); err == nil {
		return normalized
	}
	return 0
}

func contextWindowChoiceLabel(model qoder.Model, tokens int) string {
	if tokens <= 0 {
		return "自动"
	}
	for _, option := range model.SupportedContextWindows() {
		if option.TokenCount == tokens {
			if option.Label != "" {
				return option.Label
			}
			return humanTokens(tokens)
		}
	}
	return humanTokens(tokens)
}

func (s *appState) inlineContextSelector(gtx layout.Context, th *material.Theme, model qoder.Model) layout.Dimensions {
	options := model.SupportedContextWindows()
	if len(options) == 0 {
		return bodyLabel(gtx, th, humanTokens(model.MaxContextTokens()), ui.muted, 12)
	}
	selected := s.modelContextDefault(model)
	trigger := inlineClickable(inlineContextMenu.triggers, model.UpstreamID)
	if trigger.Clicked(gtx) {
		inlineReasoningMenu.openID = ""
		if inlineContextMenu.openID == model.UpstreamID {
			inlineContextMenu.openID = ""
		} else {
			inlineContextMenu.openID = model.UpstreamID
		}
	}
	maxLabel := options[len(options)-1].Label
	if maxLabel == "" {
		maxLabel = humanTokens(options[len(options)-1].TokenCount)
	}
	label := maxLabel + " · " + contextWindowChoiceLabel(model, selected)
	if inlineContextMenu.openID == model.UpstreamID {
		label += "  ▴"
	} else {
		label += "  ▾"
	}
	return roundedBackground(gtx, ui.mutedPanel, 9, func(gtx layout.Context) layout.Dimensions {
		return trigger.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 7, Bottom: 7, Left: 10, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return bodyLabel(gtx, th, label, ui.text, 12)
			})
		})
	})
}

func (s *appState) inlineContextOptions(gtx layout.Context, th *material.Theme, model qoder.Model) layout.Dimensions {
	options := model.SupportedContextWindows()
	selected := s.modelContextDefault(model)
	return roundedBackground(gtx, ui.raised, 10, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 6, Bottom: 6, Left: 6, Right: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			children := make([]layout.FlexChild, 0, (len(options)+1)*2)
			values := make([]int, 0, len(options)+1)
			values = append(values, 0)
			for _, option := range options {
				values = append(values, option.TokenCount)
			}
			for i, value := range values {
				value := value
				if i > 0 {
					children = append(children, layout.Rigid(layout.Spacer{Height: 4}.Layout))
				}
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					key := fmt.Sprintf("%s\x00%d", model.UpstreamID, value)
					button := inlineClickable(inlineContextMenu.choices, key)
					if button.Clicked(gtx) {
						s.saveModelContextDefault(model, value)
						inlineContextMenu.openID = ""
					}
					bg, fg := ui.panel, ui.muted
					if selected == value {
						bg, fg = ui.accentSoft, ui.accent
					}
					return roundedBackground(gtx, bg, 8, func(gtx layout.Context) layout.Dimensions {
						return button.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: 7, Bottom: 7, Left: 9, Right: 9}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return bodyLabel(gtx, th, contextWindowChoiceLabel(model, value), fg, 12)
							})
						})
					})
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

func (s *appState) saveModelContextDefault(model qoder.Model, tokens int) {
	if tokens > 0 {
		if _, err := model.NormalizeContextWindow(tokens); err != nil {
			s.setNotice("无法设置默认上下文: " + err.Error())
			return
		}
	}
	s.mu.RLock()
	next := s.settings
	next.ModelContextDefaults = cloneModelContextDefaults(s.settings.ModelContextDefaults)
	s.mu.RUnlock()
	if tokens <= 0 {
		delete(next.ModelContextDefaults, model.UpstreamID)
	} else {
		next.ModelContextDefaults[model.UpstreamID] = tokens
	}
	if err := desktop.SaveSettings(next); err != nil {
		s.setNotice("保存默认上下文失败: " + err.Error())
		return
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()
	s.proxy.UpdateModelContextDefaults(next.ModelContextDefaults)
	s.setNotice(fmt.Sprintf("%s 的默认上下文已设为 %s", model.DisplayName, contextWindowChoiceLabel(model, tokens)))
}
