//go:build desktop

package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/steamwo/qoder-proxy/internal/desktop"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

var ui = struct {
	canvas, panel, raised, mutedPanel, sidebar, selected color.NRGBA
	text, muted, faint, border, shadow                   color.NRGBA
	accent, accentSoft, success, successSoft             color.NRGBA
	warning, warningSoft, danger, dangerSoft             color.NRGBA
}{
	canvas: hex(0xF4F6F9), panel: hex(0xFBFCFD), raised: hex(0xFFFFFF),
	mutedPanel: hex(0xF0F3F7), sidebar: hex(0xFAFBFD), selected: hex(0xE8F0FE),
	text: hex(0x151922), muted: hex(0x5E687A), faint: hex(0x8C96A8),
	border: hex(0xDDE3EC), shadow: color.NRGBA{R: 33, G: 43, B: 58, A: 18},
	accent: hex(0x1769E8), accentSoft: hex(0xE8F0FE),
	success: hex(0x11A982), successSoft: hex(0xE5F7F1),
	warning: hex(0xE47C18), warningSoft: hex(0xFFF3E7),
	danger: hex(0xEF625B), dangerSoft: hex(0xFFF0EF),
}

func hex(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

// layout applies the product visual system before composing navigation and content.
// layout 在组合导航与内容前应用产品视觉系统。
func (s *appState) layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	th.Palette.Bg, th.Palette.Fg = ui.canvas, ui.text
	th.Palette.ContrastBg, th.Palette.ContrastFg = ui.accent, hex(0xFFFFFF)
	paint.Fill(gtx.Ops, ui.canvas)
	return layout.Inset{Top: 16, Bottom: 16, Left: 16, Right: 18}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(218), gtx.Dp(218)
				return s.sidebarLayout(gtx, th)
			}),
			layout.Rigid(layout.Spacer{Width: 22}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return s.content(gtx, th) }),
		)
	})
}

// sidebarLayout keeps navigation, runtime state, and product identity in one stable rail.
// sidebarLayout 将导航、运行状态与产品身份收纳在稳定侧栏中。
func (s *appState) sidebarLayout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return elevated(gtx, ui.sidebar, 18, layout.Inset{Top: 18, Bottom: 18, Left: 14, Right: 14}, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions { return brandMark(gtx, th) }),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Body1(th, "Qoder Proxy")
							l.Color, l.TextSize, l.Font.Weight = ui.text, 16, font.SemiBold
							return l.Layout(gtx)
						}),
					)
				})
			}),
			layout.Rigid(layout.Spacer{Height: 30}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return s.navItem(gtx, th, &s.dashBtn, pageDashboard, "仪表盘")
			}),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return s.navItem(gtx, th, &s.accountsBtn, pageAccounts, "账号额度")
			}),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return s.navItem(gtx, th, &s.modelsBtn, pageModels, "模型")
			}),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return s.navItem(gtx, th, &s.logsBtn, pageLogs, "日志") }),
			layout.Rigid(layout.Spacer{Height: 6}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return s.navItem(gtx, th, &s.settingsBtn, pageSettings, "设置")
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Spacer{}.Layout(gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return s.sidebarStatus(gtx, th) }),
		)
	})
}

func (s *appState) navItem(gtx layout.Context, th *material.Theme, btn *widget.Clickable, p page, text string) layout.Dimensions {
	bg, fg := color.NRGBA{}, ui.muted
	if s.current == p {
		bg, fg = ui.selected, ui.accent
	} else if btn.Hovered() {
		bg = ui.mutedPanel
	}
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return roundedBackground(gtx, bg, 12, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 11, Bottom: 11, Left: 13, Right: 13}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				col := ui.border
				if s.current == p {
					col = ui.accent
				}
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return statusDot(gtx, col, 7) }),
					layout.Rigid(layout.Spacer{Width: 12}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Body1(th, text)
						l.Color, l.TextSize, l.Font.Weight = fg, 14, font.Medium
						return l.Layout(gtx)
					}),
				)
			})
		})
	})
}

func (s *appState) sidebarStatus(gtx layout.Context, th *material.Theme) layout.Dimensions {
	state, detail, col := "代理已停止", "本地服务离线", ui.muted
	if s.proxy.Running() {
		state, detail, col = "代理运行中", first(s.proxy.Addr(), s.settings.Listen), ui.success
	}
	return roundedBackground(gtx, ui.mutedPanel, 12, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 12, Bottom: 12, Left: 12, Right: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return statusPill(gtx, th, state, col, color.NRGBA{}) }),
				layout.Rigid(layout.Spacer{Height: 5}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, detail, ui.faint, 12) }),
				layout.Rigid(layout.Spacer{Height: 9}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, "v"+version+" · No WebView", ui.faint, 11)
				}),
			)
		})
	})
}

func (s *appState) content(gtx layout.Context, th *material.Theme) layout.Dimensions {
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
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			switch s.current {
			case pageAccounts:
				return s.accountsPage(gtx, th)
			case pageModels:
				return s.modelsPage(gtx, th)
			case pageLogs:
				return s.logsPage(gtx, th)
			case pageSettings:
				return s.settingsPage(gtx, th)
			default:
				return s.dashboard(gtx, th)
			}
		}),
	)
}

func (s *appState) topbar(gtx layout.Context, th *material.Theme) layout.Dimensions {
	title, subtitle := "仪表盘", "实时查看代理状态、账号额度与请求健康情况。"
	switch s.current {
	case pageAccounts:
		title, subtitle = "账号额度", "管理账号并查看实时用量与重置周期。"
	case pageModels:
		title, subtitle = "模型", "查看可用模型、上下文能力与推理模式。"
	case pageLogs:
		title, subtitle = "日志", "检查代理请求、响应耗时与上游异常。"
	case pageSettings:
		title, subtitle = "设置", "配置代理、应用行为与系统集成。"
	}
	s.mu.RLock()
	feedback := s.notice
	if s.busy != "" {
		feedback = s.busy
	}
	s.mu.RUnlock()
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.H4(th, title)
					l.Color, l.TextSize, l.Font.Weight = ui.text, 28, font.SemiBold
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: 4}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, subtitle, ui.muted, 13) }),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if feedback == "" {
				return layout.Dimensions{}
			}
			return layout.Inset{Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return pill(gtx, th, feedback, ui.raised, ui.muted) })
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			state, col := "已停止", ui.muted
			if s.proxy.Running() {
				state, col = "运行中", ui.success
			}
			return s.trayStatusBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return statusPill(gtx, th, state, col, ui.raised) })
		}),
	)
}

func (s *appState) trayPanel(gtx layout.Context, th *material.Theme) layout.Dimensions {
	state, col := "代理已停止", ui.muted
	if s.proxy.Running() {
		state, col = "代理运行中", ui.success
	}
	s.mu.RLock()
	cred, quota := s.cred, s.quota
	s.mu.RUnlock()
	remaining := "等待刷新"
	if quota.User != nil {
		remaining = fmt.Sprintf("%.0f%% 可用", quota.User.RemainingPercent)
	}
	return elevated(gtx, ui.raised, 16, layout.Inset{Top: 16, Bottom: 14, Left: 16, Right: 16}, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return statusPill(gtx, th, state, col, ui.mutedPanel) }),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return keyValue(gtx, th, "端点", first(s.proxy.Addr(), s.settings.Listen))
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return keyValue(gtx, th, "账号", first(cred.Email, cred.Name, "未登录"))
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return keyValue(gtx, th, "剩余额度", remaining) }),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return secondaryButton(th, &s.trayRefreshBtn, "刷新额度").Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						text := "启动代理"
						if s.proxy.Running() {
							text = "停止代理"
						}
						return primaryButton(th, &s.trayToggleBtn, text).Layout(gtx)
					}),
				)
			}),
		)
	})
}

func (s *appState) dashboard(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return material.List(th, &s.pageList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		s.mu.RLock()
		logged, quota, quotaErr := s.loggedIn, s.quota, s.quotaErr
		s.mu.RUnlock()
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return s.runtimeCard(gtx, th, logged) }),
			layout.Rigid(layout.Spacer{Height: 16}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx,
					layout.Flexed(.8, func(gtx layout.Context) layout.Dimensions { return s.quotaOverview(gtx, th, logged, quota, quotaErr) }),
					layout.Rigid(layout.Spacer{Width: 16}.Layout),
					layout.Flexed(1.25, func(gtx layout.Context) layout.Dimensions { return s.healthOverview(gtx, th) }),
				)
			}),
		)
	})
}

func (s *appState) runtimeCard(gtx layout.Context, th *material.Theme, logged bool) layout.Dimensions {
	running := s.proxy.Running()
	state, detail, col, bg := "代理已停止", "登录并启动后即可接收本地请求", ui.muted, ui.mutedPanel
	if running {
		state, detail, col, bg = "代理运行中", first(s.proxy.Addr(), s.settings.Listen), ui.success, ui.successSoft
	}
	return elevated(gtx, bg, 18, layout.Inset{Top: 22, Bottom: 22, Left: 24, Right: 24}, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return statusPill(gtx, th, state, col, ui.raised) }),
					layout.Rigid(layout.Spacer{Height: 13}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.H5(th, detail)
						l.Color, l.TextSize, l.Font.Weight = ui.text, 20, font.SemiBold
						return l.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return runtimeMetric(gtx, th, "运行时长", formatDuration(s.proxy.Uptime()))
			}),
			layout.Rigid(layout.Spacer{Width: 26}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return runtimeMetric(gtx, th, "当前请求", fmt.Sprintf("%d", s.logbuf.Stats().Requests))
			}),
			layout.Rigid(layout.Spacer{Width: 24}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !logged {
					return primaryButton(th, &s.loginBtn, "登录 Qoder").Layout(gtx)
				}
				if running {
					return dangerButton(th, &s.toggleBtn, "停止代理").Layout(gtx)
				}
				return primaryButton(th, &s.toggleBtn, "启动代理").Layout(gtx)
			}),
		)
	})
}

func runtimeMetric(gtx layout.Context, th *material.Theme, title, value string) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, title, ui.muted, 12) }),
		layout.Rigid(layout.Spacer{Height: 6}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body1(th, value)
			l.Color, l.Font.Weight = ui.text, font.SemiBold
			return l.Layout(gtx)
		}),
	)
}

func (s *appState) quotaOverview(gtx layout.Context, th *material.Theme, logged bool, quota qoder.QuotaSnapshot, quotaErr string) layout.Dimensions {
	return card(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return sectionTitle(gtx, th, "账号额度", "当前 Qoder 账号")
			}),
			layout.Rigid(layout.Spacer{Height: 18}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !logged {
					return emptyState(gtx, th, "尚未登录", "登录后显示额度与重置周期。")
				}
				if quotaErr != "" {
					return emptyState(gtx, th, "额度读取失败", quotaErr)
				}
				if quota.User == nil {
					return emptyState(gtx, th, "等待额度数据", "点击刷新读取当前额度。")
				}
				return quotaMeter(gtx, th, quota.User, first(quota.Plan, "个人额度"))
			}),
			layout.Rigid(layout.Spacer{Height: 17}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return secondaryButton(th, &s.refreshBtn, "刷新额度").Layout(gtx)
			}),
		)
	})
}

func (s *appState) healthOverview(gtx layout.Context, th *material.Theme) layout.Dimensions {
	entries, stats := s.logbuf.Entries(5), s.logbuf.Stats()
	rate := 100.0
	if stats.Requests > 0 {
		rate = float64(stats.Requests-stats.Errors) / float64(stats.Requests) * 100
	}
	return card(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return sectionTitle(gtx, th, "请求健康", "当前进程内的实时请求")
			}),
			layout.Rigid(layout.Spacer{Height: 15}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return compactMetric(gtx, th, "请求总数", fmt.Sprintf("%d", stats.Requests), ui.text)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return compactMetric(gtx, th, "成功率", fmt.Sprintf("%.1f%%", rate), ui.success)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return compactMetric(gtx, th, "错误数", fmt.Sprintf("%d", stats.Errors), ui.warning)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return compactMetric(gtx, th, "平均耗时", fmt.Sprintf("%d ms", stats.AverageMillis), ui.text)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: 15}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if len(entries) == 0 {
					return emptyState(gtx, th, "暂无请求", "代理收到请求后会显示最近活动。")
				}
				children := make([]layout.FlexChild, 0, len(entries)*2)
				for i, entry := range entries {
					e := entry
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return miniLogRow(gtx, th, e) }))
					if i < len(entries)-1 {
						children = append(children, layout.Rigid(divider))
					}
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
			}),
		)
	})
}

func miniLogRow(gtx layout.Context, th *material.Theme, entry desktop.LogEntry) layout.Dimensions {
	status, col := logStatus(entry)
	return layout.Inset{Top: 7, Bottom: 7}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return statusDot(gtx, col, 7) }),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return bodyLabel(gtx, th, first(entry.Method, entry.Level), ui.muted, 12)
			}),
			layout.Rigid(layout.Spacer{Width: 10}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return bodyLabel(gtx, th, first(entry.Path, entry.Message, "运行事件"), ui.text, 12)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, status, col, 12) }),
		)
	})
}

func compactMetric(gtx layout.Context, th *material.Theme, title, value string, col color.NRGBA) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, title, ui.muted, 12) }),
		layout.Rigid(layout.Spacer{Height: 6}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.H6(th, value)
			l.TextSize, l.Color, l.Font.Weight = 18, col, font.SemiBold
			return l.Layout(gtx)
		}),
	)
}

func (s *appState) accountsPage(gtx layout.Context, th *material.Theme) layout.Dimensions {
	s.mu.RLock()
	cred, logged, quota, quotaErr := s.cred, s.loggedIn, s.quota, s.quotaErr
	s.mu.RUnlock()
	return material.List(th, &s.pageList).Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return elevated(gtx, ui.raised, 18, layout.Inset{Top: 22, Bottom: 22, Left: 24, Right: 24}, func(gtx layout.Context) layout.Dimensions {
					if !logged {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return sectionTitle(gtx, th, "连接 Qoder 账号", "授权后读取真实额度与模型能力。")
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return primaryButton(th, &s.loginBtn, "登录 Qoder").Layout(gtx)
							}),
						)
					}
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "当前账号", ui.muted, 12) }),
								layout.Rigid(layout.Spacer{Height: 7}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									l := material.H6(th, first(cred.Email, cred.Name, cred.UserID))
									l.Color, l.TextSize, l.Font.Weight = ui.text, 20, font.SemiBold
									return l.Layout(gtx)
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return statusPill(gtx, th, "正常", ui.success, ui.successSoft)
						}),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return secondaryButton(th, &s.refreshBtn, "刷新额度").Layout(gtx)
						}),
					)
				})
			}),
			layout.Rigid(layout.Spacer{Height: 16}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx,
					layout.Flexed(1.35, func(gtx layout.Context) layout.Dimensions {
						return card(gtx, func(gtx layout.Context) layout.Dimensions {
							return accountQuotaContent(gtx, th, logged, quota, quotaErr)
						})
					}),
					layout.Rigid(layout.Spacer{Width: 16}.Layout),
					layout.Flexed(.8, func(gtx layout.Context) layout.Dimensions { return s.accountDetailsCard(gtx, th, logged) }),
				)
			}),
		)
	})
}

func accountQuotaContent(gtx layout.Context, th *material.Theme, logged bool, quota qoder.QuotaSnapshot, quotaErr string) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionTitle(gtx, th, "额度概览", "实时额度与重置周期")
		}),
		layout.Rigid(layout.Spacer{Height: 20}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !logged {
				return emptyState(gtx, th, "尚未登录", "连接账号后查看额度。")
			}
			if quotaErr != "" {
				return emptyState(gtx, th, "读取失败", quotaErr)
			}
			if quota.User == nil {
				return emptyState(gtx, th, "等待数据", "刷新以读取当前额度。")
			}
			return quotaMeter(gtx, th, quota.User, first(quota.Plan, quota.User.Label))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if quota.Organization == nil {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: 22}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return quotaMeter(gtx, th, quota.Organization, "组织资源包")
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if quota.FetchedAt.IsZero() {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: 18}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return bodyLabel(gtx, th, "最后更新："+quota.FetchedAt.Local().Format("2006-01-02 15:04:05"), ui.faint, 12)
			})
		}),
	)
}

func (s *appState) accountDetailsCard(gtx layout.Context, th *material.Theme, logged bool) layout.Dimensions {
	s.mu.RLock()
	cred := s.cred
	s.mu.RUnlock()
	return card(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return sectionTitle(gtx, th, "账号", "当前凭据保存在本机")
			}),
			layout.Rigid(layout.Spacer{Height: 20}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !logged {
					return emptyState(gtx, th, "无活动账号", "使用 Qoder PKCE 安全登录。")
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return keyValue(gtx, th, "邮箱", first(cred.Email, "—"))
					}),
					layout.Rigid(layout.Spacer{Height: 11}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return keyValue(gtx, th, "名称", first(cred.Name, "—"))
					}),
					layout.Rigid(layout.Spacer{Height: 11}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return keyValue(gtx, th, "User ID", compactID(cred.UserID))
					}),
					layout.Rigid(layout.Spacer{Height: 20}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return dangerButton(th, &s.logoutBtn, "退出账号").Layout(gtx)
					}),
				)
			}),
		)
	})
}

func quotaMeter(gtx layout.Context, th *material.Theme, quota *qoder.QuotaWindow, title string) layout.Dimensions {
	percent := quota.RemainingPercent
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, title, ui.muted, 13) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, fmt.Sprintf("%.0f%% 可用", percent), quotaColor(percent), 13)
				}),
			)
		}),
		layout.Rigid(layout.Spacer{Height: 9}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.H4(th, fmt.Sprintf("%.0f", quota.Remaining))
			l.TextSize, l.Color, l.Font.Weight = 34, ui.text, font.SemiBold
			return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
				layout.Rigid(l.Layout),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, fmt.Sprintf("/ %.0f", quota.Limit), ui.faint, 13)
				}),
			)
		}),
		layout.Rigid(layout.Spacer{Height: 13}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return progressBar(gtx, percent/100, quotaColor(percent)) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if quota.ResetAt.IsZero() {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return bodyLabel(gtx, th, "重置时间："+quota.ResetAt.Local().Format("2006-01-02 15:04"), ui.faint, 12)
			})
		}),
	)
}

// modelsPage keeps capability discovery and per-model defaults in one focused workspace.
// modelsPage 将能力发现与逐模型默认值集中在同一工作区。
func (s *appState) modelsPage(gtx layout.Context, th *material.Theme) layout.Dimensions {
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
	ensureClickables(&s.modelRows, len(models))
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
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return s.modelTable(gtx, th, models) }),
				layout.Rigid(layout.Spacer{Width: 14}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X, gtx.Constraints.Max.X = gtx.Dp(260), gtx.Dp(260)
					index := s.selectedModel
					if index < 0 || index >= len(models) {
						index = 0
					}
					return s.modelInspector(gtx, th, models[index])
				}),
			)
		}),
	)
}

func (s *appState) modelTable(gtx layout.Context, th *material.Theme, models []qoder.Model) layout.Dimensions {
	return card(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return modelHeader(gtx, th) }),
			layout.Rigid(divider),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return material.List(th, &s.modelList).Layout(gtx, len(models), func(gtx layout.Context, i int) layout.Dimensions {
					if s.modelRows[i].Clicked(gtx) {
						s.selectedModel = i
					}
					return s.modelRows[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions { return modelRow(gtx, th, models[i], i == s.selectedModel) })
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, fmt.Sprintf("共 %d 个公开模型", len(models)), ui.faint, 12)
				})
			}),
		)
	})
}

func modelHeader(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Inset{Bottom: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "模型", ui.faint, 12) }),
			layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "上下文", ui.faint, 12) }),
			layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "输出", ui.faint, 12) }),
			layout.Flexed(.65, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "推理", ui.faint, 12) }),
		)
	})
}

func modelRow(gtx layout.Context, th *material.Theme, model qoder.Model, selected bool) layout.Dimensions {
	bg := color.NRGBA{}
	if selected {
		bg = ui.selected
	}
	return roundedBackground(gtx, bg, 10, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 12, Bottom: 12, Left: 10, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(th, model.DisplayName)
					l.Color, l.Font.Weight = ui.text, font.Medium
					return l.Layout(gtx)
				}),
				layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, humanTokens(model.MaxInputTokens), ui.muted, 12)
				}),
				layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, humanTokens(model.MaxOutputTokens), ui.muted, 12)
				}),
				layout.Flexed(.65, func(gtx layout.Context) layout.Dimensions {
					if model.IsReasoning {
						return pill(gtx, th, "Thinking", ui.accentSoft, ui.accent)
					}
					return pill(gtx, th, "标准", ui.mutedPanel, ui.muted)
				}),
			)
		})
	})
}

// modelInspector combines live capabilities with a durable per-model default at the point of decision.
// modelInspector 在用户决策位置结合实时能力与持久化的逐模型默认值。
func (s *appState) modelInspector(gtx layout.Context, th *material.Theme, model qoder.Model) layout.Dimensions {
	efforts := model.SupportedReasoningEfforts()
	thinking := "不支持"
	if len(efforts) > 0 {
		thinking = strings.Join(efforts, " · ")
	}
	options := []string{""}
	if model.SupportsReasoningDisabled() {
		options = append(options, "none")
	}
	options = append(options, efforts...)
	ensureClickables(&s.modelEffortBtns, len(options))
	s.mu.RLock()
	selectedEffort := s.settings.ModelReasoningDefaults[model.UpstreamID]
	s.mu.RUnlock()
	// Invalid saved values behave and appear as automatic after live capabilities change.
	// 实时能力变化后，无效的已保存值在行为与展示上都回退为自动。
	if selectedEffort != "" {
		if normalized, err := model.NormalizeReasoningEffort(selectedEffort); err != nil || normalized == "" {
			selectedEffort = ""
		}
	}
	return card(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return sectionTitle(gtx, th, "模型详情", "来自 Qoder 的实时元数据")
			}),
			layout.Rigid(layout.Spacer{Height: 20}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return keyValue(gtx, th, "公开名称", model.DisplayName)
			}),
			layout.Rigid(layout.Spacer{Height: 12}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return keyValue(gtx, th, "上游 ID", compactID(model.UpstreamID))
			}),
			layout.Rigid(layout.Spacer{Height: 12}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return keyValue(gtx, th, "最大输入", humanTokens(model.MaxInputTokens))
			}),
			layout.Rigid(layout.Spacer{Height: 12}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return keyValue(gtx, th, "最大输出", humanTokens(model.MaxOutputTokens))
			}),
			layout.Rigid(layout.Spacer{Height: 12}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return keyValue(gtx, th, "推理级别", thinking) }),
			layout.Rigid(layout.Spacer{Height: 16}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return bodyLabel(gtx, th, "默认思考等级", ui.text, 13)
			}),
			layout.Rigid(layout.Spacer{Height: 4}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return bodyLabel(gtx, th, "请求未指定时使用；显式值优先", ui.faint, 11)
			}),
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, reasoningOptionChildren(th, s, model, options, selectedEffort)...)
			}),
			layout.Rigid(layout.Spacer{Height: 16}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return statusPill(gtx, th, "当前可用", ui.success, ui.successSoft)
			}),
		)
	})
}

// reasoningOptionChildren uses stable clickables because Gio routes clicks on the following frame.
// reasoningOptionChildren 使用稳定的点击状态，因为 Gio 会在后续帧分发点击事件。
func reasoningOptionChildren(th *material.Theme, s *appState, model qoder.Model, options []string, selected string) []layout.FlexChild {
	children := make([]layout.FlexChild, 0, len(options)*2)
	for index, effort := range options {
		index, effort := index, effort
		if index > 0 {
			children = append(children, layout.Rigid(layout.Spacer{Height: 6}.Layout))
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if s.modelEffortBtns[index].Clicked(gtx) {
				s.saveModelReasoningDefault(model, effort)
			}
			active := selected == effort
			background, foreground := ui.mutedPanel, ui.muted
			if active {
				background, foreground = ui.accentSoft, ui.accent
			}
			return roundedBackground(gtx, background, 9, func(gtx layout.Context) layout.Dimensions {
				return s.modelEffortBtns[index].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: 7, Bottom: 7, Left: 10, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return bodyLabel(gtx, th, reasoningEffortLabel(effort), foreground, 12)
					})
				})
			})
		}))
	}
	return children
}

// reasoningEffortLabel keeps protocol values stable while presenting concise Chinese product language.
// reasoningEffortLabel 保持协议值稳定，同时展示简洁的中文产品文案。
func reasoningEffortLabel(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "":
		return "自动（跟随上游）"
	case "none":
		return "关闭"
	case "minimal":
		return "最小 · minimal"
	case "low":
		return "低 · low"
	case "medium":
		return "中 · medium"
	case "high":
		return "高 · high"
	case "max", "xhigh":
		return "最高 · " + effort
	default:
		return effort
	}
}

// logsPage presents the searchable recent tail while making disk persistence explicit.
// logsPage 展示可搜索的近期日志，并明确告知用户日志会持久化到磁盘。
func (s *appState) logsPage(gtx layout.Context, th *material.Theme) layout.Dimensions {
	entries := s.logbuf.Entries(500)
	query := strings.ToLower(strings.TrimSpace(s.logSearchEditor.Text()))
	if query != "" {
		filtered := entries[:0]
		for _, entry := range entries {
			if strings.Contains(strings.ToLower(entry.Raw), query) {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}
	ensureClickables(&s.logRows, len(entries))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return searchField(gtx, th, &s.logSearchEditor, "搜索路径、模型或状态码")
				}),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return secondaryButton(th, &s.openLogFromPageBtn, "打开日志目录").Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return secondaryButton(th, &s.clearLogsBtn, "清空日志").Layout(gtx)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return bodyLabel(gtx, th, "日志持久化到本机，文件上限 4 MB；清空会同时删除磁盘内容。", ui.faint, 11)
			})
		}),
		layout.Rigid(layout.Spacer{Height: 14}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return card(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return logHeader(gtx, th) }),
					layout.Rigid(divider),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						if len(entries) == 0 {
							return emptyState(gtx, th, "暂无日志", "运行代理后，请求与上游事件会显示在这里。")
						}
						return material.List(th, &s.logList).Layout(gtx, len(entries), func(gtx layout.Context, i int) layout.Dimensions {
							if s.logRows[i].Clicked(gtx) {
								s.selectedLog = i
							}
							return s.logRows[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return logTableRow(gtx, th, entries[i], i == s.selectedLog)
							})
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if len(entries) == 0 {
							return layout.Dimensions{}
						}
						index := s.selectedLog
						if index < 0 || index >= len(entries) {
							index = 0
						}
						return layout.Inset{Top: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return logDetails(gtx, th, entries[index]) })
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return bodyLabel(gtx, th, fmt.Sprintf("显示 %d 条", len(entries)), ui.faint, 12)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									cb := material.CheckBox(th, &s.logAutoScroll, "自动滚动")
									cb.Color = ui.accent
									return cb.Layout(gtx)
								}),
							)
						})
					}),
				)
			})
		}),
	)
}

// logHeader exposes reasoning beside the model because both define upstream behavior.
// logHeader 将思考等级放在模型旁，因为二者共同决定上游行为。
func logHeader(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Inset{Bottom: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{}.Layout(gtx,
			layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "时间", ui.faint, 12) }),
			layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "状态", ui.faint, 12) }),
			layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "方法", ui.faint, 12) }),
			layout.Flexed(1.45, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "路径 / 事件", ui.faint, 12) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "模型", ui.faint, 12) }),
			layout.Flexed(.7, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "思考等级", ui.faint, 12) }),
			layout.Flexed(.65, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "耗时", ui.faint, 12) }),
		)
	})
}

// logTableRow shows the final upstream effort rather than the raw client preference.
// logTableRow 展示最终上游思考等级，而不是客户端原始偏好。
func logTableRow(gtx layout.Context, th *material.Theme, entry desktop.LogEntry, selected bool) layout.Dimensions {
	bg := color.NRGBA{}
	if selected {
		bg = ui.selected
	}
	status, col := logStatus(entry)
	stamp, duration := "—", "—"
	if !entry.Time.IsZero() {
		stamp = entry.Time.Local().Format("15:04:05")
	}
	if entry.DurationMS > 0 {
		duration = fmt.Sprintf("%d ms", entry.DurationMS)
	}
	return roundedBackground(gtx, bg, 9, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 10, Bottom: 10, Left: 8, Right: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, stamp, ui.muted, 12) }),
				layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions { return statusPill(gtx, th, status, col, color.NRGBA{}) }),
				layout.Flexed(.55, func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, first(entry.Method, "—"), ui.muted, 12)
				}),
				layout.Flexed(1.45, func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, first(entry.Path, entry.Message, "运行事件"), ui.text, 12)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, first(entry.Model, "—"), ui.muted, 12)
				}),
				layout.Flexed(.7, func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, first(entry.ReasoningEffort, "—"), ui.muted, 12)
				}),
				layout.Flexed(.65, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, duration, ui.muted, 12) }),
			)
		})
	})
}

// logDetails surfaces the parsed effort before the raw line so operators can verify it quickly.
// logDetails 将解析后的思考等级置于原始行之前，便于运维人员快速核对。
func logDetails(gtx layout.Context, th *material.Theme, entry desktop.LogEntry) layout.Dimensions {
	return roundedBackground(gtx, ui.mutedPanel, 12, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 12, Bottom: 12, Left: 14, Right: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, "请求详情", ui.text, 13) }),
				layout.Rigid(layout.Spacer{Height: 6}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return keyValue(gtx, th, "最终思考等级", first(entry.ReasoningEffort, "未记录"))
				}),
				layout.Rigid(layout.Spacer{Height: 8}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Caption(th, entry.Raw)
					l.Color, l.MaxLines = ui.muted, 3
					return l.Layout(gtx)
				}),
			)
		})
	})
}

func logStatus(entry desktop.LogEntry) (string, color.NRGBA) {
	if entry.Status >= 500 || strings.EqualFold(entry.Level, "ERROR") {
		if entry.Status > 0 {
			return fmt.Sprintf("%d", entry.Status), ui.danger
		}
		return "错误", ui.danger
	}
	if entry.Status >= 400 || strings.EqualFold(entry.Level, "WARN") {
		if entry.Status > 0 {
			return fmt.Sprintf("%d", entry.Status), ui.warning
		}
		return "警告", ui.warning
	}
	if entry.Status > 0 {
		return fmt.Sprintf("%d", entry.Status), ui.success
	}
	return first(entry.Level, "信息"), ui.muted
}

// settingsPage presents one continuous workspace so entering Settings never reveals another navigation layer.
// settingsPage 使用单一连续工作区，避免进入“设置”后再次出现一层设置导航。
func (s *appState) settingsPage(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(th, &s.settingsList).Layout(gtx, 3, func(gtx layout.Context, index int) layout.Dimensions {
				switch index {
				case 0:
					return layout.Inset{Bottom: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Start}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return card(gtx, func(gtx layout.Context) layout.Dimensions { return s.generalSettings(gtx, th) })
							}),
							layout.Rigid(layout.Spacer{Width: 14}.Layout),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return card(gtx, func(gtx layout.Context) layout.Dimensions { return s.traySettings(gtx, th) })
							}),
						)
					})
				case 1:
					return layout.Inset{Bottom: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return card(gtx, func(gtx layout.Context) layout.Dimensions { return s.proxySettings(gtx, th) })
					})
				default:
					return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return card(gtx, func(gtx layout.Context) layout.Dimensions { return s.dataSettings(gtx, th) })
					})
				}
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return elevated(gtx, ui.raised, 14, layout.Inset{Top: 12, Bottom: 12, Left: 16, Right: 16}, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return bodyLabel(gtx, th, "更改将安全保存到本机；服务参数在下次启动代理时生效。", ui.muted, 12)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return primaryButton(th, &s.saveBtn, "保存全部更改").Layout(gtx)
						}),
					)
				})
			})
		}),
	)
}

// generalSettings keeps startup behavior and local authentication together because both affect app access.
// generalSettings 将启动行为与本地鉴权放在一起，因为二者都会影响应用访问方式。
func (s *appState) generalSettings(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionTitle(gtx, th, "常规", "应用启动与本地访问")
		}),
		layout.Rigid(layout.Spacer{Height: 22}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return checkRow(gtx, th, &s.autoStart, "启动时自动运行代理", "仅在账号凭据有效时启动。")
		}),
		layout.Rigid(layout.Spacer{Height: 18}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return field(gtx, th, "本地 API Key", "留空则不启用本地鉴权", &s.apiKeyEditor)
		}),
	)
}

// proxySettings isolates network-facing values so their operational risk remains obvious.
// proxySettings 集中网络相关参数，使其运行风险保持清晰可见。
func (s *appState) proxySettings(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionTitle(gtx, th, "代理", "监听、队列与故障恢复")
		}),
		layout.Rigid(layout.Spacer{Height: 22}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return field(gtx, th, "监听地址", "例如 127.0.0.1:9000", &s.listenEditor)
		}),
		layout.Rigid(layout.Spacer{Height: 17}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return field(gtx, th, "队列重试次数", "例如 20", &s.retriesEditor)
				}),
				layout.Rigid(layout.Spacer{Width: 14}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return field(gtx, th, "总等待上限", "例如 10m", &s.maxWaitEditor)
				}),
			)
		}),
		layout.Rigid(layout.Spacer{Height: 17}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return roundedBackground(gtx, ui.warningSoft, 10, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 10, Bottom: 10, Left: 12, Right: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, "将监听地址改为 0.0.0.0 会向局域网开放端口，请确认防火墙配置。", ui.warning, 12)
				})
			})
		}),
	)
}

// traySettings explains that closing the window preserves the service when tray mode is enabled.
// traySettings 明确说明启用托盘后关闭窗口仍会保留服务运行。
func (s *appState) traySettings(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionTitle(gtx, th, "系统与托盘", "控制桌面集成与通知行为")
		}),
		layout.Rigid(layout.Spacer{Height: 22}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return checkRow(gtx, th, &s.minimizeToTray, "关闭窗口后驻留托盘", "下次启动时创建托盘图标；关闭主窗口不会停止代理。")
		}),
		layout.Rigid(layout.Spacer{Height: 18}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return checkRow(gtx, th, &s.trayNotifications, "显示详细托盘提示", "在悬停提示中显示账号与剩余额度。")
		}),
		layout.Rigid(layout.Spacer{Height: 20}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return roundedBackground(gtx, ui.mutedPanel, 12, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 14, Bottom: 14, Left: 14, Right: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return sectionTitle(gtx, th, "托盘交互", "双击打开主窗口，右键控制代理与刷新额度。")
				})
			})
		}),
	)
}

// dataSettings names every persisted artifact and explains its privacy and retention semantics.
// dataSettings 列出每个持久化数据，并解释其隐私属性与保留语义。
func (s *appState) dataSettings(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionTitle(gtx, th, "数据与隐私", "本应用只在本机保存运行所需数据")
		}),
		layout.Rigid(layout.Spacer{Height: 18}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return dataPathCard(gtx, th, "账号凭据", s.dataPaths.Credentials, "包含访问令牌与账号标识 · 敏感 · 退出登录时删除", ui.warningSoft, ui.warning)
		}),
		layout.Rigid(layout.Spacer{Height: 10}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return dataPathCard(gtx, th, "桌面设置", s.dataPaths.Settings, "包含代理参数与可选本地 API Key · 敏感", ui.accentSoft, ui.accent)
		}),
		layout.Rigid(layout.Spacer{Height: 10}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return dataPathCard(gtx, th, "运行日志", s.dataPaths.Log, "不记录请求正文或凭据 · 自动限制为 4 MB · 可随时清空", ui.successSoft, ui.success)
		}),
		layout.Rigid(layout.Spacer{Height: 16}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return secondaryButton(th, &s.openConfigBtn, "打开配置目录").Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return secondaryButton(th, &s.openLogsBtn, "打开日志目录").Layout(gtx)
				}),
			)
		}),
	)
}

// dataPathCard prioritizes the real absolute path because it is the actionable product detail.
// dataPathCard 优先展示真实绝对路径，因为这是用户最需要操作的信息。
func dataPathCard(gtx layout.Context, th *material.Theme, title, path, detail string, bg, accent color.NRGBA) layout.Dimensions {
	return roundedBackground(gtx, bg, 12, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 12, Bottom: 12, Left: 14, Right: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body1(th, title)
					l.Color, l.Font.Weight, l.TextSize = accent, font.SemiBold, 13
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: 5}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Caption(th, path)
					l.Color, l.MaxLines = ui.text, 2
					return l.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: 5}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, detail, ui.muted, 11) }),
			)
		})
	})
}

func checkRow(gtx layout.Context, th *material.Theme, value *widget.Bool, title, detail string) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, title, ui.text, 14) }),
				layout.Rigid(layout.Spacer{Height: 4}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, detail, ui.muted, 12) }),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			cb := material.CheckBox(th, value, "")
			cb.Color = ui.accent
			return cb.Layout(gtx)
		}),
	)
}

func card(gtx layout.Context, child layout.Widget) layout.Dimensions {
	return elevated(gtx, ui.raised, 16, layout.Inset{Top: 20, Bottom: 20, Left: 20, Right: 20}, child)
}

func elevated(gtx layout.Context, bg color.NRGBA, radius unit.Dp, inset layout.Inset, child layout.Widget) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			push := op.Offset(image.Pt(0, gtx.Dp(3))).Push(gtx.Ops)
			defer push.Pop()
			return roundedBackground(gtx, ui.shadow, radius, layout.Spacer{}.Layout)
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return roundedBackground(gtx, bg, radius, func(gtx layout.Context) layout.Dimensions { return inset.Layout(gtx, child) })
		}),
	)
}

func roundedBackground(gtx layout.Context, bg color.NRGBA, radius unit.Dp, child layout.Widget) layout.Dimensions {
	minimum := gtx.Constraints.Min
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			r := gtx.Dp(radius)
			defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Min}, r).Push(gtx.Ops).Pop()
			paint.Fill(gtx.Ops, bg)
			return layout.Dimensions{Size: gtx.Constraints.Min}
		},
		func(gtx layout.Context) layout.Dimensions { gtx.Constraints.Min = minimum; return child(gtx) },
	)
}

func sectionTitle(gtx layout.Context, th *material.Theme, title, subtitle string) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.H6(th, title)
			l.TextSize, l.Color, l.Font.Weight = 16, ui.text, font.SemiBold
			return l.Layout(gtx)
		}),
		layout.Rigid(layout.Spacer{Height: 4}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, subtitle, ui.muted, 12) }),
	)
}

func primaryButton(th *material.Theme, button *widget.Clickable, text string) material.ButtonStyle {
	b := material.Button(th, button, text)
	b.Background, b.Color, b.CornerRadius = ui.accent, hex(0xFFFFFF), 11
	b.Inset, b.Font.Weight = (layout.Inset{Top: 10, Bottom: 10, Left: 16, Right: 16}), font.Medium
	return b
}

func secondaryButton(th *material.Theme, button *widget.Clickable, text string) material.ButtonStyle {
	b := material.Button(th, button, text)
	b.Background, b.Color, b.CornerRadius = ui.mutedPanel, ui.text, 11
	b.Inset, b.Font.Weight = (layout.Inset{Top: 10, Bottom: 10, Left: 15, Right: 15}), font.Medium
	return b
}

func dangerButton(th *material.Theme, button *widget.Clickable, text string) material.ButtonStyle {
	b := material.Button(th, button, text)
	b.Background, b.Color, b.CornerRadius = ui.danger, hex(0xFFFFFF), 11
	b.Inset, b.Font.Weight = (layout.Inset{Top: 10, Bottom: 10, Left: 16, Right: 16}), font.Medium
	return b
}

func pill(gtx layout.Context, th *material.Theme, text string, bg, fg color.NRGBA) layout.Dimensions {
	return roundedBackground(gtx, bg, 9, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 5, Bottom: 5, Left: 9, Right: 9}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return bodyLabel(gtx, th, text, fg, 12)
		})
	})
}

func statusPill(gtx layout.Context, th *material.Theme, text string, col, bg color.NRGBA) layout.Dimensions {
	return roundedBackground(gtx, bg, 12, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 7, Bottom: 7, Left: 11, Right: 11}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return statusDot(gtx, col, 8) }),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, text, col, 12) }),
			)
		})
	})
}

func progressBar(gtx layout.Context, fraction float64, col color.NRGBA) layout.Dimensions {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	h, w := gtx.Dp(7), gtx.Constraints.Max.X
	gtx.Constraints.Min, gtx.Constraints.Max.Y = image.Pt(w, h), h
	defer clip.UniformRRect(image.Rect(0, 0, w, h), h/2).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ui.mutedPanel)
	fill := int(float64(w) * fraction)
	if fill > 0 {
		defer clip.UniformRRect(image.Rect(0, 0, fill, h), h/2).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, col)
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func statusDot(gtx layout.Context, col color.NRGBA, size unit.Dp) layout.Dimensions {
	d := gtx.Dp(size)
	defer clip.Ellipse(image.Rect(0, 0, d, d)).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, col)
	return layout.Dimensions{Size: image.Pt(d, d)}
}

func brandMark(gtx layout.Context, th *material.Theme) layout.Dimensions {
	d := gtx.Dp(30)
	gtx.Constraints.Min, gtx.Constraints.Max = image.Pt(d, d), image.Pt(d, d)
	return roundedBackground(gtx, ui.accent, 9, func(gtx layout.Context) layout.Dimensions {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Body1(th, "Q")
			l.Color, l.Font.Weight = hex(0xFFFFFF), font.SemiBold
			return l.Layout(gtx)
		})
	})
}

func field(gtx layout.Context, th *material.Theme, title, hint string, editor *widget.Editor) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, title, ui.text, 13) }),
		layout.Rigid(layout.Spacer{Height: 7}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return roundedBackground(gtx, ui.mutedPanel, 10, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 10, Bottom: 10, Left: 12, Right: 12}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					e := material.Editor(th, editor, hint)
					e.Color, e.HintColor, e.TextSize = ui.text, ui.faint, 14
					return e.Layout(gtx)
				})
			})
		}),
	)
}

func searchField(gtx layout.Context, th *material.Theme, editor *widget.Editor, hint string) layout.Dimensions {
	return roundedBackground(gtx, ui.raised, 11, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 10, Bottom: 10, Left: 13, Right: 13}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			e := material.Editor(th, editor, hint)
			e.Color, e.HintColor, e.TextSize = ui.text, ui.faint, 13
			return e.Layout(gtx)
		})
	})
}

func emptyState(gtx layout.Context, th *material.Theme, title, detail string) layout.Dimensions {
	return roundedBackground(gtx, ui.mutedPanel, 11, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 14, Bottom: 14, Left: 14, Right: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, title, ui.text, 13) }),
				layout.Rigid(layout.Spacer{Height: 4}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Caption(th, detail)
					l.Color, l.MaxLines = ui.muted, 3
					return l.Layout(gtx)
				}),
			)
		})
	})
}

func keyValue(gtx layout.Context, th *material.Theme, key, value string) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return bodyLabel(gtx, th, key, ui.muted, 12) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Caption(th, value)
			l.Color, l.Font.Weight, l.MaxLines = ui.text, font.Medium, 1
			return l.Layout(gtx)
		}),
	)
}

func bodyLabel(gtx layout.Context, th *material.Theme, text string, col color.NRGBA, size unit.Sp) layout.Dimensions {
	l := material.Body2(th, text)
	l.Color, l.TextSize = col, size
	return l.Layout(gtx)
}

func divider(gtx layout.Context) layout.Dimensions {
	h, w := gtx.Dp(1), gtx.Constraints.Max.X
	paint.FillShape(gtx.Ops, ui.border, clip.Rect{Max: image.Pt(w, h)}.Op())
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func ensureClickables(items *[]widget.Clickable, count int) {
	if len(*items) >= count {
		return
	}
	*items = append(*items, make([]widget.Clickable, count-len(*items))...)
}

func quotaColor(percent float64) color.NRGBA {
	if percent <= 15 {
		return ui.danger
	}
	if percent <= 40 {
		return ui.warning
	}
	return ui.success
}

func humanTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.0fK", float64(n)/1000)
	}
	if n <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d", n)
}

func compactID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 20 {
		return first(value, "—")
	}
	return value[:8] + "…" + value[len(value)-8:]
}

func formatDuration(value time.Duration) string {
	if value <= 0 {
		return "—"
	}
	hours, minutes, seconds := int(value.Hours()), int(value.Minutes())%60, int(value.Seconds())%60
	if hours > 0 {
		return fmt.Sprintf("%d 小时 %d 分", hours, minutes)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}
