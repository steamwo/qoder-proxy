# Desktop design system

The desktop UI is a native Gio implementation shared by Windows and Linux. It deliberately avoids WebView-only effects and treats translucency as layered alpha surfaces so the same hierarchy remains legible when compositor blur is unavailable.

## Principles

- Runtime state is the first decision on Dashboard and remains visible in the top status control and sidebar footer.
- Cool gray surfaces and restrained blue interaction color provide the primary hierarchy; mint is reserved for healthy state, amber for warnings, and coral for destructive or failed state.
- Cards use 16–18 dp radii, a low-alpha offset shadow, and 20–24 dp internal spacing.
- Navigation labels are consistent across all five destinations: 仪表盘, 账号额度, 模型, 日志, 设置.
- UI values come from the existing credential, quota, model registry, proxy manager, and structured log buffer. The visual layer must not invent unsupported quota dimensions or routing features.

## Interaction notes

- The top status control opens an in-app summary with endpoint, account, remaining quota, refresh, and start/stop.
- Windows tray double-click opens the main window; the context menu exposes open, start/stop, refresh quota, and exit.
- Linux StatusNotifierItem activation opens the main window; secondary activation starts or stops the proxy.
- Model and log rows are selectable. Their details appear in a stable inspector instead of a modal.
- Settings are grouped into 常规, 代理, and 系统与托盘. Changes that affect process startup or listeners take effect on the next relevant start.
