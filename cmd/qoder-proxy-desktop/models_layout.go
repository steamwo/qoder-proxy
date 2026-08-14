//go:build desktop

package main

import (
	"strings"

	"gioui.org/layout"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

var inlineReasoningMenu = struct {
	openID   string
	triggers map[string]*widget.Clickable
	choices  map[string]*widget.Clickable
}{
	triggers: make(map[string]*widget.Clickable),
	choices:  make(map[string]*widget.Clickable),
}

func inlineClickable(store map[string]*widget.Clickable, key string) *widget.Clickable {
	if button := store[key]; button != nil {
		return button
	}
	button := new(widget.Clickable)
	store[key] = button
	return button
}

func (s *appState) modelsContentV2(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return s.topbar(gtx, th) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !s.trayPopover {
				return layout.Spacer{Height: 18}.Layout(gtx)
			}
			return layout.Inset{Top: 10, Bottom: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = gtx.Dp(330)
					return s.trayPanel(gtx, th)
				})
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return s.modelsPageV2(gtx, th) }),
	)
}

// modelsPageV2 keeps live model capabilities and actionable defaults in the same list.
// modelsPageV2 将模型实时能力与可操作的默认参数放在同一列表中。
func (s *appState) modelsPageV2(gtx layout.Context, th *material.Theme) layout.Dimensions {
	s.mu.RLock()
	models, errText := append([]qoder.Model(nil), s.models...), s.modelsErr
	s.mu.RUnlock()

	query := strings.ToLower(strings.TrimSpace(s.modelSearchEditor.Text()))
	if query != "" {
		filtered := models[:0]
		for _, model := range models {
			if strings.Contains(strings.ToLower(model.DisplayName+" "+model.UpstreamID), query) {
				filtered = append(filtered, model)
			}
		}
		models = filtered
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return searchField(gtx, th, &s.modelSearchEditor, "搜索模型")
				}),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return secondaryButton(th, &s.refreshBtn, "刷新模型").Layout(gtx)
				}),
			)
		}),
		layout.Rigid(layout.Spacer{Height: 14}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if errText != "" {
				return card(gtx, func(gtx layout.Context) layout.Dimensions { return emptyState(gtx, th, "模型获取失败", errText) })
			}
			if len(models) == 0 {
				return card(gtx, func(gtx layout.Context) layout.Dimensions {
					return emptyState(gtx, th, "暂无模型", "登录并刷新后显示 Qoder 当前可用模型。")
				})
			}
			return s.modelTableV2(gtx, th, models)
		}),
	)
}

func (s *appState) modelTableV2(gtx layout.Context, th *material.Theme, models []qoder.Model) layout.Dimensions {
	return card(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return modelHeaderV2(gtx, th) }),
			layout.Rigid(divider),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return material.List(th, &s.modelList).Layout(gtx, len(models), func(gtx layout.Context, i int) layout.Dimensions {
					return s.modelRowV2(gtx, th, models[i])
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, "上下文列显示 Qoder 实时最大档位，并可在支持时设置默认窗口；输出与思考能力同样来自实时模型配置。", ui.faint, 11)
				})
			}),
		)
	})
}

func modelHeaderV2(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Inset{Bottom: 10, Left: 10, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{}.Layout(gtx,
			layout.Flexed(1.25, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "模型", ui.faint, 12) }),
			layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "上下文上限", ui.faint, 12) }),
			layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "输出上限", ui.faint, 12) }),
			layout.Flexed(.9, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "默认思考", ui.faint, 12) }),
		)
	})
}

func (s *appState) modelRowV2(gtx layout.Context, th *material.Theme, model qoder.Model) layout.Dimensions {
	return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return roundedBackground(gtx, ui.panel, 10, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 10, Bottom: 10, Left: 10, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				children := []layout.FlexChild{
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1.25, func(gtx layout.Context) layout.Dimensions {
								return bodyLabel(gtx, th, model.DisplayName, ui.text, 13)
							}),
							layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions {
								return s.inlineContextSelector(gtx, th, model)
							}),
							layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions {
								return bodyLabel(gtx, th, humanTokens(model.MaxOutputTokens), ui.muted, 12)
							}),
							layout.Flexed(.9, func(gtx layout.Context) layout.Dimensions {
								return s.inlineReasoningSelector(gtx, th, model)
							}),
						)
					}),
				}
				if inlineContextMenu.openID == model.UpstreamID {
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.W.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								if max := gtx.Dp(280); gtx.Constraints.Max.X > max {
									gtx.Constraints.Max.X = max
								}
								return s.inlineContextOptions(gtx, th, model)
							})
						})
					}))
				}
				if inlineReasoningMenu.openID == model.UpstreamID {
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								if max := gtx.Dp(280); gtx.Constraints.Max.X > max {
									gtx.Constraints.Max.X = max
								}
								return s.inlineReasoningOptions(gtx, th, model)
							})
						})
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
			})
		})
	})
}

func (s *appState) inlineReasoningSelector(gtx layout.Context, th *material.Theme, model qoder.Model) layout.Dimensions {
	options := modelReasoningOptions(model)
	if len(options) <= 1 && !model.SupportsReasoningDisabled() && len(model.SupportedReasoningEfforts()) == 0 {
		return pill(gtx, th, "不支持", ui.mutedPanel, ui.muted)
	}

	selected := s.modelReasoningDefault(model)
	trigger := inlineClickable(inlineReasoningMenu.triggers, model.UpstreamID)
	if trigger.Clicked(gtx) {
		inlineContextMenu.openID = ""
		if inlineReasoningMenu.openID == model.UpstreamID {
			inlineReasoningMenu.openID = ""
		} else {
			inlineReasoningMenu.openID = model.UpstreamID
		}
	}
	label := reasoningEffortCompactLabel(selected)
	if inlineReasoningMenu.openID == model.UpstreamID {
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

func (s *appState) inlineReasoningOptions(gtx layout.Context, th *material.Theme, model qoder.Model) layout.Dimensions {
	options := modelReasoningOptions(model)
	selected := s.modelReasoningDefault(model)
	return roundedBackground(gtx, ui.raised, 10, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 6, Bottom: 6, Left: 6, Right: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			children := make([]layout.FlexChild, 0, len(options)*2)
			for i, effort := range options {
				effort := effort
				if i > 0 {
					children = append(children, layout.Rigid(layout.Spacer{Height: 4}.Layout))
				}
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					key := model.UpstreamID + "\x00" + effort
					button := inlineClickable(inlineReasoningMenu.choices, key)
					if button.Clicked(gtx) {
						s.saveModelReasoningDefault(model, effort)
						inlineReasoningMenu.openID = ""
					}
					bg, fg := ui.panel, ui.muted
					if selected == effort {
						bg, fg = ui.accentSoft, ui.accent
					}
					return roundedBackground(gtx, bg, 8, func(gtx layout.Context) layout.Dimensions {
						return button.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: 7, Bottom: 7, Left: 9, Right: 9}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return bodyLabel(gtx, th, reasoningEffortLabel(effort), fg, 12)
							})
						})
					})
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

func modelReasoningOptions(model qoder.Model) []string {
	options := []string{""}
	if model.SupportsReasoningDisabled() {
		options = append(options, "none")
	}
	return append(options, model.SupportedReasoningEfforts()...)
}

func (s *appState) modelReasoningDefault(model qoder.Model) string {
	s.mu.RLock()
	selected := s.settings.ModelReasoningDefaults[model.UpstreamID]
	s.mu.RUnlock()
	if selected == "" {
		return ""
	}
	if normalized, err := model.NormalizeReasoningEffort(selected); err == nil && normalized != "" {
		return normalized
	}
	return ""
}

func reasoningEffortCompactLabel(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "":
		return "自动"
	case "none":
		return "关闭"
	case "minimal":
		return "最小"
	case "low":
		return "低"
	case "medium":
		return "中"
	case "high":
		return "高"
	case "max", "xhigh":
		return "最高"
	default:
		return effort
	}
}
