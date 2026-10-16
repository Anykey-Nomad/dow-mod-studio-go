module dow-mod-studio

go 1.27.1

require (
	gioui.org v0.10.2
	gioui.org/shader v1.0.9
	github.com/ebitengine/oto/v3 v3.5.0
	github.com/fogleman/fauxgl v0.0.0-20250110135958-abf826acbbbd
	github.com/fsnotify/fsnotify v1.10.1
	golang.org/x/image v0.26.0
	golang.org/x/sys v0.47.0
)

require (
	github.com/christerso/vulkan-go v0.0.0-20260618152204-bff25e5b7646 // indirect
	github.com/ebitengine/purego v0.11.0 // indirect
	github.com/fogleman/simplify v0.0.0-20170216171241-d32f302d5046 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/jfreymuth/pulse v0.1.3 // indirect
	golang.org/x/exp/shiny v0.0.0-20250408133849-7e4ce0ab07d0 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/text v0.32.0 // indirect
)

replace gioui.org => ../gio

replace github.com/christerso/vulkan-go => ../gio/third_party/vulkan-go

replace gioui.org/shader => ../gio/third_party/gio-shader
