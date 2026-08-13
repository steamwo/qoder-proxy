//go:build desktop

package main

import (
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/paint"
	"gioui.org/widget/material"
)

// renderFrame keeps the existing application shell while routing Settings and Models
// through their refined layouts.
// renderFrame 保留现有应用外壳，并为“设置”和“模型”使用优化后的布局。
func (s *appState) renderFrame(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if s.current != pageSettings && s.current != pageModels {
		return s.layout(gtx, th)
	}

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
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				if s.current == pageModels {
					return s.modelsContentV2(gtx, th)
				}
				return s.settingsContentV2(gtx, th)
			}),
		)
	})
}

func (s *appState) settingsContentV2(gtx layout.Context, th *material.Theme) layout.Dimensions {
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
			return s.settingsPageV2(gtx, th)
		}),
	)
}

// settingsPageV2 uses one visual surface and separators instead of a collection of cards.
// settingsPageV2 使用单一视觉面板和分隔线，避免多个高低不一的卡片。
func (s *appState) settingsPageV2(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return elevated(gtx, ui.raised, 16, layout.Inset{Top: 4, Bottom: 4, Left: 22, Right: 22}, func(gtx layout.Context) layout.Dimensions {
				return material.List(th, &s.settingsList).Layout(gtx, 4, func(gtx layout.Context, index int) layout.Dimensions {
					var section layout.Widget
					switch index {
					case 0:
						section = func(gtx layout.Context) layout.Dimensions { return s.generalSettings(gtx, th) }
					case 1:
						section = func(gtx layout.Context) layout.Dimensions { return s.proxySettings(gtx, th) }
					case 2:
						section = func(gtx layout.Context) layout.Dimensions { return s.traySettingsV2(gtx, th) }
					default:
						section = func(gtx layout.Context) layout.Dimensions { return s.dataSettingsV2(gtx, th) }
					}

					children := make([]layout.FlexChild, 0, 3)
					if index > 0 {
						children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: 2, Bottom: 22}.Layout(gtx, divider)
						}))
					}
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						bottom := layout.Inset{Top: 18, Bottom: 24}
						if index == 3 {
							bottom.Bottom = 18
						}
						return bottom.Layout(gtx, section)
					}))
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
				})
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 14, Left: 4, Right: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return bodyLabel(gtx, th, "更改将安全保存到本机；服务参数在下次启动代理时生效。", ui.muted, 12)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return primaryButton(th, &s.saveBtn, "保存全部更改").Layout(gtx)
					}),
				)
			})
		}),
	)
}

func (s *appState) traySettingsV2(gtx layout.Context, th *material.Theme) layout.Dimensions {
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
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return statusDot(gtx, ui.accent, 7) }),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return bodyLabel(gtx, th, "托盘交互：双击打开主窗口，右键控制代理与刷新额度。", ui.muted, 12)
				}),
			)
		}),
	)
}

func (s *appState) dataSettingsV2(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionTitle(gtx, th, "数据与隐私", "本应用只在本机保存运行所需数据")
		}),
		layout.Rigid(layout.Spacer{Height: 12}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return dataPathRow(gtx, th, "账号凭据", s.dataPaths.Credentials, "包含访问令牌与账号标识 · 敏感 · 退出登录时删除", ui.warning)
		}),
		layout.Rigid(divider),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return dataPathRow(gtx, th, "桌面设置", s.dataPaths.Settings, "包含代理参数与可选本地 API Key · 敏感", ui.accent)
		}),
		layout.Rigid(divider),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return dataPathRow(gtx, th, "运行日志", s.dataPaths.Log, "不记录请求正文或凭据 · 自动限制为 4 MB · 可随时清空", ui.success)
		}),
		layout.Rigid(layout.Spacer{Height: 14}.Layout),
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

func dataPathRow(gtx layout.Context, th *material.Theme, title, path, detail string, accent color.NRGBA) layout.Dimensions {
	return layout.Inset{Top: 11, Bottom: 11}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Start}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 5}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return statusDot(gtx, accent, 7)
				})
			}),
			layout.Rigid(layout.Spacer{Width: 12}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Body1(th, title)
						l.Color, l.TextSize, l.Font.Weight = ui.text, 13, font.SemiBold
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: 4}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Caption(th, path)
						l.Color, l.MaxLines = ui.text, 2
						return l.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: 4}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return bodyLabel(gtx, th, detail, ui.muted, 11)
					}),
				)
			}),
		)
	})
}
