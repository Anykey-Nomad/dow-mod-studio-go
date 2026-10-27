package app

import (
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/applog"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"fmt"
	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func Run() {
	if len(os.Args) > 1 && os.Args[1] == "--debug-markers" {
		os.Exit(runMarkerDebugCLI(os.Args[2:], os.Stdout, os.Stderr))
	}

	initLogger()
	defer closeLogger()
	defer handlePanic()

	// Before anything is loaded: the collector's pacing is set once, and a
	// reader who wants Go's own can still say so in the environment.
	tuneCollector()

	// Handle file-association CLI flags before GUI starts.
	// Flags are private/undocumented besides --help; they are used by installer/runner.
	if len(os.Args) > 1 {
		switch strings.ToLower(os.Args[1]) {
		case "--register", "--register-assoc", "/register", "-register":
			if err := registerModuleAssociation(); err != nil {
				log.Printf("register failed: %v", err)
				fmt.Fprintf(os.Stderr, "register failed: %v\n", err)
				exitLogged(1)
			}
			msg := fmt.Sprintf("Registered .module association for %s", getExePath())
			log.Printf("%s", msg)
			fmt.Println(msg)
			exitLogged(0)
		case "--unregister", "--unregister-assoc", "--remove-assoc", "/unregister":
			if err := unregisterModuleAssociation(); err != nil {
				log.Printf("unregister failed: %v", err)
				fmt.Fprintf(os.Stderr, "unregister failed: %v\n", err)
				exitLogged(1)
			}
			log.Printf("Removed .module association")
			fmt.Println("Removed .module association")
			exitLogged(0)
		case "--assoc-status", "--status":
			ok, detail, _ := isModuleAssociated()
			if ok {
				log.Printf("assoc-status: associated true (%s)", detail)
				fmt.Printf("associated: true (%s)\n", detail)
			} else {
				if detail != "" {
					log.Printf("assoc-status: associated false (current: %q)", detail)
					fmt.Printf("associated: false (current: %q)\n", detail)
				} else {
					log.Printf("assoc-status: associated false")
					fmt.Printf("associated: false\n")
				}
			}
			exitLogged(0)
		case "--help", "-h", "/?":
			fmt.Println("Usage:")
			fmt.Println("  dow-mod-studio.exe [path/to/mod.module]")
			fmt.Println("  dow-mod-studio.exe --register        Register .module file association (current user, HKCU)")
			fmt.Println("  dow-mod-studio.exe --unregister      Remove .module file association")
			fmt.Println("  dow-mod-studio.exe --assoc-status    Show association status")
			fmt.Println("  dow-mod-studio.exe --debug-markers --module <file> --unit <name|path>  Dump model marker coordinates as JSON")
			log.Printf("help shown, exiting 0; log=%q", applog.LogFilePath)
			exitLogged(0)
		}
	}

	go func() {
		defer handlePanic()
		w := new(app.Window)
		w.Option(app.Title("Corsix's Mod Studio 0.5.5  \u2014  DoW Mod Studio Go (Gio)"), app.Size(unit.Dp(1280), unit.Dp(760)))
		if err := run(w); err != nil {
			log.Printf("run error: %v", err)
		}
		exitLogged(0)
	}()
	app.Main()
}

func run(w *app.Window) error {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	th.TextSize = unit.Sp(13)
	th.Palette = material.Palette{
		Bg:         uikit.CBg,
		Fg:         uikit.CText,
		ContrastBg: uikit.CSaveBg,
		ContrastFg: uikit.CText,
	}

	ui := newUI(th)
	ui.Win = w
	var ops op.Ops

	state, stateExists, stateErr := loadPersistedState()
	if stateErr != nil {
		log.Printf("load state: %v", stateErr)
	}
	if stateExists {
		ApplyPersistedSettings(ui, state)
	}
	// Gio creates its window GPU on the first frame. Install the persisted
	// choice before receiving that frame; otherwise Windows' Vulkan driver
	// sees an empty DOW_GPU_API and falls through to EGL.
	if err := render.SetGPUAPI(ui.GpuAPI); err != nil {
		return err
	}

	// An explicit module argument always wins over the saved session.
	if findFirstModuleArg() != "" {
		TryAutoLoad(ui)
	} else if !stateExists || !RestorePersistedState(ui, state) {
		TryAutoLoad(ui)
	}
	// A file the OS handed over (a registered association) opens once the
	// module is in place: the .whe lookup needs the unit catalog, and the
	// viewers read through the tree where the file lives in one.
	if arg := findFirstAssocArg(); arg != "" {
		OpenAssociatedFile(ui, arg)
	}

	for {
		e := w.Event()
		switch e := e.(type) {
		case app.DestroyEvent:
			if err := savePersistedState(ui); err != nil {
				log.Printf("save state on exit: %v", err)
			}
			StopModFileWatcher(ui)
			CloseAllFileTabs(ui)
			if ui.ModTree != nil {
				ui.ModTree.Close()
			}
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			MaybeSaveState(ui)
			PollModCatalog(ui)
			PollImageDecodes(ui)
			PollModelJobs(ui)
			PollUnitModel(ui)
			HandleInput(ui, gtx)
			Layout(ui, gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func Layout(ui *uiState, gtx layout.Context) layout.Dimensions {
	// window bg
	fillRect(gtx, uikit.CBg, gtx.Constraints.Max)

	// keep visibleNodes up to date for Layout to render
	ui.VisibleNodes = GetVisibleNodes(ui)
	EnsureRowClicks(ui, len(ui.VisibleNodes))
	EnsureRowHover(ui, len(ui.VisibleNodes))

	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutMenuBar(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CBorder, 1) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if len(ui.OpenTabs) == 0 {
						return layout.Dimensions{}
					}
					return LayoutTabBar(ui, gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if len(ui.OpenTabs) == 0 {
						return layout.Dimensions{}
					}
					return hLine(gtx, uikit.CBorder, 1)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutContent(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutStatusBar(ui, gtx) }),
			)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if ui.MenuOpen < 0 {
				return layout.Dimensions{}
			}
			return LayoutMenuDropdown(ui, gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions { return LayoutSearchOverlay(ui, gtx) }),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions { return LayoutLocaleOverlay(ui, gtx) }),
		// The tab context menu is the last thing drawn, so it sits above the
		// bar it was opened from and above any other menu that is up.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.TabContextVisible {
				return layout.Dimensions{}
			}
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					pass := pointer.PassOp{}.Push(gtx.Ops)
					dims := ui.TabContextDismiss.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: gtx.Constraints.Max}
					})
					pass.Pop()
					return dims
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions { return LayoutTabContextMenu(ui, gtx) }),
			)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.FilePickOpen {
				return layout.Dimensions{}
			}
			return LayoutFilePicker(ui, gtx)
		}),
		// The event picker sits above the import dialog: an event field
		// opens it from inside the Events section.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.EvtPick.Open {
				return layout.Dimensions{}
			}
			return LayoutEvtPicker(ui, gtx)
		}),
		// The new unit dialog is above the open dialog: it is opened from the
		// object window, and a dialog that writes a file should be the one
		// thing the window is answering to.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.NewObjDialog {
				return layout.Dimensions{}
			}
			return LayoutNewObjDialog(ui, gtx)
		}),
		// The localisation editor is above the other dialogs: it is opened
		// from inside the object window, so anything already up would have to
		// be let go of first.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.LocaleEditDialog {
				return layout.Dimensions{}
			}
			return LayoutLocaleEditDialog(ui, gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.SettingsDialog {
				return layout.Dimensions{}
			}
			return LayoutSettingsDialog(ui, gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.FileDeleteDialog {
				return layout.Dimensions{}
			}
			return LayoutFileDeleteDialog(ui, gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.ClearRecycleDialog {
				return layout.Dimensions{}
			}
			return LayoutClearRecycleDialog(ui, gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !ui.ArchiveDialog {
				return layout.Dimensions{}
			}
			return LayoutArchiveDialog(ui, gtx)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if ui.RgdTooltipText == "" {
				return layout.Dimensions{}
			}
			return layout.NE.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return widget.Border{Color: uikit.CBorder, Width: unit.Dp(1), CornerRadius: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(10), ui.RgdTooltipText)
						l.Color = uikit.CText
						return l.Layout(gtx)
					})
				})
			})
		}),
	)
}

func LayoutMiddlePanel(ui *uiState, gtx layout.Context) layout.Dimensions {
	tab := ActiveTab(ui)
	if tab != nil && tab.Kind == "fda" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutFdaPlayer(ui, gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.Kind == "wav" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutWavViewer(ui, gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.Kind == "whm" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutWhmViewer(ui, gtx, tab) }),
			)
		})
	}
	// Scenario map (.sgb) viewer
	if tab != nil && tab.Kind == "sgb" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutSgbViewer(ui, gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.Kind == "image" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutImageViewer(ui, gtx, tab) }),
			)
		})
	}
	// Archive listing (.sga)
	if tab != nil && tab.Kind == "sga" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutSgaListing(ui, gtx, tab) }),
			)
		})
	}
	// Text/script editor (rat, con, lua, ai)
	if tab != nil && tab.Kind == "text" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutTextEditor(ui, gtx, tab) }),
			)
		})
	}
	// RGD or Module view
	if tab != nil && tab.Kind == "rgd" {
		return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if tab.RgdFile == nil {
						if tab.RgdError != "" {
							return layout.Inset{Top: unit.Dp(16), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								col := rgb(0xB00020)
								l := material.Label(ui.Th, unit.Sp(11), "Failed: "+tab.RgdError)
								l.Color = col
								return l.Layout(gtx)
							})
						}
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							l := material.Label(ui.Th, unit.Sp(11), "No RGD — this tab is empty")
							l.Color = uikit.CTextDim
							return l.Layout(gtx)
						})
					}
					if len(tab.RgdMidNodes) == 0 {
						RebuildRgdMidNodesFor(ui, tab)
						EnsureRgdMidClicks(ui, tab, len(tab.RgdMidNodes))
					}
					return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return LayoutListWithScrollbar(ui, gtx, &tab.RgdMidScroll, &tab.RgdMidList, 22, len(tab.RgdMidNodes), func(gtx layout.Context, index int) layout.Dimensions {
							v := tab.RgdMidNodes[index]
							return LayoutRgdMidRow(ui, gtx, tab, index, v)
						})
					})
				}),
			)
		})
	}
	// Module tab or empty
	return PanelWithScroll(ui, gtx, &ui.MiddleScroll, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MiddleHeader(ui, gtx) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					// Keep the empty-state message inside the panel with a clear left margin.
					gtx.Constraints.Min.X += gtx.Dp(unit.Dp(16))
					gtx.Constraints.Max.X -= gtx.Dp(unit.Dp(16))
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return centeredPanelLabel(gtx, ui.Th, "No file selected", unit.Sp(11), uikit.CTextDim)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return centeredPanelLabel(gtx, ui.Th, "Module tab — click a .rgd/.fda/.jpg/.tga/.dds file in the left tree to open in a new tab", unit.Sp(10), uikit.CTextDim)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if len(ui.OpenTabs) > 0 {
								return centeredPanelLabel(gtx, ui.Th, fmt.Sprintf("%d file tab(s) open — switch via the top tab bar", len(ui.OpenTabs)), unit.Sp(10), uikit.CTextDim)
							}
							return layout.Dimensions{}
						}),
					)
				})
			}),
		)
	})
}

func LayoutRightPanel(ui *uiState, gtx layout.Context) layout.Dimensions {
	tab := ActiveTab(ui)
	if tab != nil && tab.Kind == "sgb" {
		return LayoutSgbObjectPanel(ui, gtx, tab)
	}
	// The object tab keeps this panel, but there is no open file behind it, so
	// it describes the selected unit instead of a file's properties.
	if isUnitTab(tab) {
		return PanelWithScroll(ui, gtx, &ui.RightScroll, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "Object") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitInfoTable(ui, gtx) }),
			}
			if v := UnitModelViewer(ui); v != nil && v.Model != nil {
				children = append(children,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "Model") }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(ui.Th, unit.Sp(9), UnitModelFileLabel(ui))
						l.Color = uikit.CTextDim
						l.MaxLines = 2
						return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8), Bottom: unit.Dp(2)}.Layout(gtx, l.Layout)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return UnitModelFrameLabel(ui, gtx, v) }),
				)
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
	if tab != nil && tab.Kind == "fda" {
		return PanelWithScroll(ui, gtx, &ui.RightScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "FDA Properties") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return FdaPropertiesTable(ui, gtx, tab) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "File Info") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return FdaInfoTable(ui, gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.Kind == "wav" {
		return PanelWithScroll(ui, gtx, &ui.RightScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "WAV Properties") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return WavInfoTable(ui, gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.Kind == "whm" {
		return PanelWithScroll(ui, gtx, &ui.RightScroll, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "Model Properties") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return WhmInfoTable(ui, gtx, tab) }),
			}
			if tab.Model != nil {
				children = append(children,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "3D Preview") }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ModelInfoTable(ui, gtx, tab) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "Materials by Mesh") }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ModelMaterialDiagnosticsTable(ui, gtx, tab) }),
				)
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
	if tab != nil && tab.Kind == "image" {
		return PanelWithScroll(ui, gtx, &ui.RightScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "Image Properties") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ImageInfoTable(ui, gtx, tab) }),
			)
		})
	}
	if tab != nil && tab.Kind == "rgd" {
		return PanelWithScroll(ui, gtx, &ui.RightScroll, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "Properties") }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return PropertiesTable(ui, gtx, tab) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "Table Children") }),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return ChildrenTable(ui, gtx, tab) }),
			)
		})
	}
	return PanelWithScroll(ui, gtx, &ui.RightScroll, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "Properties") }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return centeredPanelLabel(gtx, ui.Th, "Module tab — open a file to see its properties", unit.Sp(10), uikit.CTextDim)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return hLine(gtx, uikit.CHeaderLine, 1) }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionHeader(gtx, ui.Th, "Table Children") }),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return centeredPanelLabel(gtx, ui.Th, "—", unit.Sp(10), uikit.CTextDim)
			}),
		)
	})
}

func HandleSaveActiveTab(ui *uiState) {
	if t := ActiveTab(ui); t != nil && t.Kind == "sgb" {
		HandleSgbSave(ui, t)
	} else if t := ActiveTab(ui); t != nil && t.Kind == "rgd" {
		HandleRgdSave(ui, t)
	} else if t := ActiveTab(ui); t != nil && t.Kind == "text" {
		HandleTextSave(ui, t)
	} else if t := ActiveTab(ui); t != nil && t.Kind == "fda" {
		FdaExportWav(ui)
	} else if t := ActiveTab(ui); t != nil && t.Kind == "wav" {
		ConvertWavToFDA(ui, t)
	} else if t := ActiveTab(ui); t != nil && t.Kind == "whm" {
		ConvertWhmToSgmFile(ui, t)
	} else if ui.ModTree == nil {
		ui.StatusText = "Save — no mod loaded"
	} else if ActiveTab(ui) == nil {
		ui.StatusText = "Save — open an editable file first"
	} else {
		ui.StatusText = "Save — the active file type is not editable"
	}
}

func HandleKeyboard(ui *uiState, gtx layout.Context) {
	if ui.SearchVisible {
		// The result list is the focus while the search panel is open.
		return
	}
	if ui.ContextMenuVisible {
		for {
			ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
			if !ok {
				break
			}
			e, ok := ev.(key.Event)
			if !ok {
				continue
			}
			if e.State == key.Press {
				CloseFileContextMenu(ui)
				return
			}
		}
	}
	if TextActive(ui) {
		return
	}
	if tab := ActiveTab(ui); tab != nil && tab.Kind == "sgb" && tab.SgbView3D && tab.Model != nil && tab.SgbCameraTag != nil && gtx.Focused(tab.SgbCameraTag) {
		for {
			ev, ok := gtx.Event(key.Filter{Focus: tab.SgbCameraTag, Name: key.NameDeleteForward})
			if !ok {
				break
			}
			ke, ok := ev.(key.Event)
			if !ok || ke.State != key.Press {
				continue
			}
			DeleteSelectedSgbEntities(ui, tab)
			return
		}
		// The focused map viewer consumes its own arrows after input handling;
		// do not also move the file-tree selection on the same key press.
		return
	}
	if TreeHasKeyboard(ui) {
		for _, name := range treeActionKeyNames {
			for {
				ev, ok := gtx.Event(key.Filter{Name: name})
				if !ok {
					break
				}
				e, ok := ev.(key.Event)
				if !ok || e.State != key.Press {
					continue
				}
				DeleteSelectedTreeFile(ui)
				return
			}
		}
	}
	for _, name := range treeNavKeyNames {
		for {
			ev, ok := gtx.Event(key.Filter{Name: name})
			if !ok {
				break
			}
			e, ok := ev.(key.Event)
			if !ok || e.State != key.Press {
				continue
			}
			switch ui.KeyboardArea {
			case "rgd":
				if isEnterKey(name) {
					ActivateSelectedRgdNode(ui, gtx)
				} else {
					NavigateRgdTree(ui, name)
				}
			default:
				NavigateFileTree(ui, name)
			}
		}
	}
}

func NavigateFileTree(ui *uiState, name key.Name) {
	if ui.ModTree == nil || len(ui.VisibleNodes) == 0 {
		return
	}
	if isEnterKey(name) {
		OpenSelectedTreeFile(ui)
		return
	}
	idx := 0
	for i, node := range ui.VisibleNodes {
		if node.FullPath == ui.SelectedTreePath {
			idx = i
			break
		}
	}
	switch name {
	case key.NameUpArrow:
		if idx > 0 {
			idx--
		}
	case key.NameDownArrow:
		if idx+1 < len(ui.VisibleNodes) {
			idx++
		}
	case key.NameLeftArrow:
		node := ui.VisibleNodes[idx]
		if node.IsDir && node.HasKids && ui.Expanded[node.FullPath] {
			delete(ui.Expanded, node.FullPath)
			break
		}
		for i := idx - 1; i >= 0; i-- {
			if ui.VisibleNodes[i].Depth < node.Depth {
				idx = i
				break
			}
		}
	case key.NameRightArrow:
		node := ui.VisibleNodes[idx]
		if node.IsDir && node.HasKids && !ui.Expanded[node.FullPath] {
			ui.Expanded[node.FullPath] = true
		}
	}
	node := ui.VisibleNodes[idx]
	ui.SelectedTreePath = node.FullPath
	if node.IsFile {
		ui.SelectedPath = node.FullPath
	} else {
		ui.SelectedPath = ""
	}
	ui.LeftList.ScrollTo(idx)
	MarkStateDirty(ui)
}

func OpenSelectedTreeFile(ui *uiState) {
	for _, n := range ui.VisibleNodes {
		if n.FullPath == ui.SelectedTreePath {
			OpenTreeFile(ui, n)
			return
		}
	}
}

func OpenTreeFile(ui *uiState, n modtree.VisibleNode) {
	if !n.IsFile {
		return
	}
	ui.SelectedPath = n.FullPath
	ui.SelectedTreePath = n.FullPath
	ui.StatusText = fmt.Sprintf("%s  —  %s  [%s]", n.FullPath, n.ModName, kindLabel(n.Kind))
	// The formats that need a viewer of their own are opened by name, and
	// everything else is offered to the text editor. Keeping the two apart
	// is what lets the editor's list be a setting: a module can carry
	// scripts of any naming, and none of them need a case added here.
	//
	// The image formats are asked about as a list rather than spelled out as
	// a case, because that list is the same one the decoders answer to.
	// Spelling it out here is how .rtx and .rsh ended up decodable but
	// unopenable.
	if isImageViewerFile(n.FullPath) {
		OpenImage(ui, n.FullPath)
		MarkStateDirty(ui)
		return
	}
	switch strings.ToLower(filepath.Ext(n.FullPath)) {
	case ".rgd":
		OpenRgd(ui, n.FullPath)
		MarkStateDirty(ui)
		return
	case ".fda":
		OpenFda(ui, n.FullPath)
		MarkStateDirty(ui)
		return
	case ".wav":
		OpenWav(ui, n.FullPath)
		MarkStateDirty(ui)
		return
	case ".sgb":
		OpenSgb(ui, n.FullPath)
		MarkStateDirty(ui)
		return
	case ".whm", ".sgm":
		OpenWhm(ui, n.FullPath)
		MarkStateDirty(ui)
		return
	}
	if IsEditorFile(ui, n.FullPath) {
		OpenTextFile(ui, n.FullPath)
	}
	MarkStateDirty(ui)
}

func ActivateSelectedRgdNode(ui *uiState, gtx layout.Context) {
	tab := ActiveTab(ui)
	if tab == nil || tab.Kind != "rgd" || tab.RgdFile == nil {
		return
	}
	RgdFlushPending(ui, tab)
	var selected *rgdVisible
	for i := range tab.RgdMidNodes {
		if tab.RgdMidNodes[i].Path == tab.RgdSelected {
			selected = &tab.RgdMidNodes[i]
			break
		}
	}
	if selected == nil || selected.Entry == nil {
		return
	}
	if refPath := FindRgdReferenceFile(ui, selected.Ref); refPath != "" {
		file := ui.ModTree.FindFile(refPath)
		if file != nil {
			OpenTreeFile(ui, modtree.VisibleNode{
				Name:     file.Name,
				FullPath: file.FullPath,
				IsFile:   true,
				ModName:  file.Source.ModName,
				Kind:     file.Source.Kind,
			})
			return
		}
	}
	// RGD entries do not have a separate file to open. Keep the entry selected,
	// reveal its properties, and focus the editable property when possible.
	ui.RightScroll.Offset = 0
	if selected.IsTable {
		gtx.Execute(key.FocusCmd{Tag: &tab.RgdRefEditor})
	} else if selected.Entry.Type != rgd.RgdTypeBool && rgdIsEditableType(selected.Entry.Type) {
		ed := RgdEnsureValEditor(ui, tab, tab.RgdSelected, selected.Entry.DisplayValue())
		gtx.Execute(key.FocusCmd{Tag: ed})
	}
	ui.StatusText = "Properties: " + tab.RgdSelected
}

func ActivateTreeRow(ui *uiState, index int, n modtree.VisibleNode) {
	ui.KeyboardArea = "files"
	ui.SelectedTreePath = n.FullPath
	if n.IsDir {
		ui.SelectedPath = ""
		if n.HasKids {
			if ui.Expanded[n.FullPath] {
				delete(ui.Expanded, n.FullPath)
			} else {
				ui.Expanded[n.FullPath] = true
			}
		}
	} else if n.IsFile {
		if IsExternalTextFile(ui, n.FullPath) && ui.LastTreeClickPath == n.FullPath && time.Since(ui.LastTreeClickAt) < 350*time.Millisecond {
			ui.LastTreeClickPath = ""
			ui.LastTreeClickAt = time.Time{}
			if err := OpenTreeFileExternally(ui, n.FullPath); err != nil {
				ui.StatusText = fmt.Sprintf("Open with system application failed: %v", err)
			}
		} else {
			ui.LastTreeClickPath = n.FullPath
			ui.LastTreeClickAt = time.Now()
			OpenTreeFile(ui, n)
		}
	} else {
		ui.LastTreeClickPath = ""
		ui.LastTreeClickAt = time.Time{}
	}
	MarkStateDirty(ui)
}

// handleTreePointerInput is the file tree's pointer handling.
//
// Every row owns a gesture.Click over its own area, registered by layoutTreeRow
// when the row is laid out, and Gio decides what a click is: a press and a
// release that belong to the same row. Nothing here remembers that a press
// happened, and there is no distance to measure — a gesture that starts on one
// row and is released on another is cancelled by Gio, because the row it
// started on was left behind, which is the whole of what a drag over a list
// has to mean.
//
// What a gesture cannot see is a right press: a click is the primary button by
// definition, and Gio drops any other button. The file menu therefore still
// comes from the one area the tree keeps over the whole list, where a press of
// the secondary button names the row it landed on.
func HandleTreePointerInput(ui *uiState, gtx layout.Context, nodes []modtree.VisibleNode) {
	for i := range ui.RowClicks {
		if i >= len(nodes) {
			break
		}
		click := &ui.RowClicks[i]
		for {
			ev, ok := click.Update(gtx.Source)
			if !ok {
				break
			}
			if ev.Kind == gesture.KindClick && !ui.ContextMenuVisible {
				ActivateTreeRow(ui, i, nodes[i])
			}
		}
		if i < len(ui.RowHover) {
			ui.RowHover[i] = click.Hovered()
		}
	}
	if ui.LeftTreePointerTag == nil {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: ui.LeftTreePointerTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || !e.Buttons.Contain(pointer.ButtonSecondary) {
			continue
		}
		row := TreeRowAt(ui, e.Position.Y)
		if row >= 0 && row < len(nodes) && nodes[row].IsFile {
			OpenFileContextMenu(ui, nodes[row], row)
		}
	}
}

func HandleInput(ui *uiState, gtx layout.Context) {
	// The new unit dialog writes a file, so it is read before everything else
	// and nothing behind it is read while it is up.
	if ui.NewObjDialog {
		HandleNewObjDialogInput(ui, gtx)
		return
	}
	if ui.FileDeleteDialog {
		if ui.FileDeleteConfirm.Clicked(gtx) {
			path := ui.FileDeletePath
			ui.FileDeleteDialog = false
			ui.FileDeletePath = ""
			if err := DeleteCurrentModFile(ui, path); err != nil {
				ui.StatusText = fmt.Sprintf("Delete failed: %v", err)
			}
		} else if ui.FileDeleteCancel.Clicked(gtx) {
			ui.FileDeleteDialog = false
			ui.FileDeletePath = ""
			ui.StatusText = "File deletion cancelled"
		} else {
			// The dialog can be reached from the keyboard, so it has to be
			// leaveable from there too.
			for {
				ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
				if !ok {
					break
				}
				if e, ok := ev.(key.Event); ok && e.State == key.Press {
					ui.FileDeleteDialog = false
					ui.FileDeletePath = ""
					ui.StatusText = "File deletion cancelled"
					break
				}
			}
		}
		return
	}
	if ui.ClearRecycleDialog {
		if ui.ClearRecycleConfirm.Clicked(gtx) {
			ui.ClearRecycleDialog = false
			dir, err := ensureRecycleBinDir()
			if err == nil {
				err = clearRecycleDir(dir)
			}
			if err != nil {
				ui.StatusText = fmt.Sprintf("Empty Recycle Bin failed: %v", err)
			} else {
				ui.StatusText = "Recycle Bin emptied"
			}
		} else if ui.ClearRecycleCancel.Clicked(gtx) {
			ui.ClearRecycleDialog = false
			ui.StatusText = "Recycle Bin cleanup cancelled"
		}
		return
	}
	// The localisation editor is modal over the object window: it owns the keys
	// and the clicks, so nothing behind it is read while it is up. It sits above
	// the other dialogs in the stack, so it is resolved before them.
	if ui.LocaleEditDialog {
		HandleLocaleEditInput(ui, gtx)
		return
	}
	if ui.SettingsDialog {
		for {
			if _, ok := ui.SettingsGameEditor.Update(gtx); !ok {
				break
			}
		}
		for {
			if _, ok := ui.SettingsFormatEditor.Update(gtx); !ok {
				break
			}
		}
		if ui.SettingsThemeClick.Clicked(gtx) {
			ui.DarkTheme = !ui.DarkTheme
			applyMaterialTheme(ui.Th, ui.DarkTheme)
			MarkStateDirty(ui)
		}
		for i := range ui.AssocClicks {
			if ui.AssocClicks[i].Clicked(gtx) {
				ToggleAssociation(ui, i)
			}
		}
		if ui.SettingsOk.Clicked(gtx) {
			ui.GamePath = strings.TrimSpace(ui.SettingsGameEditor.Text())
			// The format list is read here rather than as it is typed,
			// so a half-finished entry never changes what a click opens.
			ui.TextEditorFormats = parseTextFormats(ui.SettingsFormatEditor.Text())
			// A new game folder means a different set of .ucs files, so the
			// language list and the string table are both rebuilt from it.
			RefreshLocales(ui)
			ui.SettingsDialog = false
			MarkStateDirty(ui)
		}
		if ui.SettingsCancel.Clicked(gtx) {
			ui.DarkTheme = ui.SettingsThemeOriginal
			ui.GamePath = ui.SettingsGameOriginal
			ui.TextEditorFormats = parseTextFormats(ui.SettingsFormatOriginal)
			applyMaterialTheme(ui.Th, ui.DarkTheme)
			ui.SettingsDialog = false
		}
		return
	}
	// The file dialog is modal and is shared by Open Module, the timeline
	// import and the sound field, so it answers before the rest of the input.
	if ui.FilePickOpen {
		HandleFilePicker(ui, gtx)
		return
	}
	if ui.ArchiveDialog {
		HandleArchiveDialogInput(ui, gtx)
		return
	}
	// The event picker is modal over the Events section the same way.
	if ui.EvtPick.Open {
		HandleEvtPickerInput(ui, gtx)
		return
	}
	// The object interface is a tab, so its pickers and rows are part of the
	// ordinary input path rather than a modal branch that swallows everything.
	// The tool button that opens it is polled on every frame, which is why it
	// runs before the rest.
	// The unit screen names are read in the background and arrive in batches.
	// Nothing else keeps the window repainting, so this has to ask for the
	// redraw itself or a list would sit on file names until the mouse moved.
	if PollUnitNames(ui) {
		gtx.Execute(op.InvalidateCmd{})
	}
	// The squads a unit belongs to are indexed in the background for the same
	// reason: the index is built by walking 431 squad files, and nothing else
	// would repaint the window when the results arrived.
	if PollUnitSquads(ui) {
		gtx.Execute(op.InvalidateCmd{})
	}
	// The references between a mod's units, buildings, weapons and abilities
	// are read in the background too, and for the same reason: reading six
	// hundred files takes seconds, and a relationships list that filled in
	// quietly would otherwise sit half-empty until the mouse moved.
	if PollGraphLinks(ui) {
		gtx.Execute(op.InvalidateCmd{})
	}
	HandleUnitToolInput(ui, gtx)
	if UnitTabActive(ui) {
		HandleUnitTabInput(ui, gtx)
	}
	// The language picker hangs off the menu bar like the menus and the search
	// field, and it puts both of them away when it opens, so it is polled first
	// and leaves the rest of the input alone when it is closed.
	HandleLocaleInput(ui, gtx)
	// The tab bar's wheel and right click, and then whatever the context menu
	// they opened wants. Both come before the tab clicks so a right press on a
	// tab is not also read as selecting it.
	HandleTabBarInput(ui, gtx)
	HandleTabContextMenuInput(ui, gtx)
	// An Alt+click on a resolved string was seen while the row was laid out;
	// opening the .ucs is what the next frame does with it.
	if ui.LocalePending != "" {
		id := ui.LocalePending
		ui.LocalePending = ""
		JumpToLocaleEntry(ui, id)
	}
	// Alt+Ctrl on a localised row, or a click on one with no text, was seen the
	// same way. The request is acted on here because putting a dialog up is the
	// input pass's business, not a layout's.
	if req := ui.LocalePendingRequest; req.Action != localeRowNothing {
		ui.LocalePendingRequest = localeEditRequest{}
		switch req.Action {
		case localeRowJump:
			JumpToLocaleEntry(ui, req.Id)
		case localeRowEdit, localeRowCreate:
			// A press in the file view knows its own entry; one in the object
			// window has to be looked up among the unit's rows.
			field := ui.LocalePendingField
			if field == nil {
				if d := CurrentUnitDetails(ui); d != nil {
					field = localeFieldForID(d, req.Id)
				}
			}
			fieldKey := ui.LocalePendingFieldKey
			if fieldKey == "" && field != nil {
				fieldKey = localeRowKey(field, "")
			}
			ui.LocalePendingField = nil
			ui.LocalePendingFieldKey = ""
			OpenLocaleEdit(ui, req.Id, req.Action, field, fieldKey)
		}
	}
	// The search field owns the arrow and Enter keys while it is open, so it
	// has to run before the tree's keyboard handling.
	HandleSearchInput(ui, gtx)
	HandleKeyboard(ui, gtx)

	for i := range ui.MenuClicks {
		if ui.MenuClicks[i].Clicked(gtx) {
			CloseFileContextMenu(ui)
			// A menu and the search panel both hang off the menu bar, so
			// opening a menu puts the search away.
			CloseSearch(ui)
			if ui.MenuOpen == i {
				ui.MenuOpen = -1
			} else {
				ui.MenuOpen = i
			}
			ui.RecentMenuExpanded = false
		}
	}
	if ui.MenuOpen >= 0 {
		items := MenuItemsForOpen(ui)
		EnsureMenuItemClicks(ui, ui.MenuOpen, len(items))
		for j := range ui.MenuItemClicks[ui.MenuOpen] {
			if ui.MenuItemClicks[ui.MenuOpen][j].Clicked(gtx) {
				if items[j] != "—" {
					label := items[j]
					if ui.MenuOpen == 0 && label == "Recent Modules" {
						ui.RecentMenuExpanded = !ui.RecentMenuExpanded
						continue
					}
					if ui.MenuOpen == 0 && ui.RecentMenuExpanded {
						header := recentModulesHeaderIndex()
						nameIndex := j - header - 2
						if header >= 0 && nameIndex >= 0 && nameIndex < len(ui.RecentModules) {
							LoadModule(ui, ui.RecentModules[nameIndex])
							ui.MenuOpen = -1
							ui.RecentMenuExpanded = false
							break
						}
					}
					if ui.MenuOpen == 0 && isEmptyRecycleBinMenuLabel(label) {
						ui.ClearRecycleDialog = true
					} else if ui.MenuOpen == 0 {
						switch label {
						case "Open Module...":
							OpenModulePicker(ui)
						case "Recent Modules":
							ui.RecentMenuExpanded = !ui.RecentMenuExpanded
							continue
						case "View Recycle Bin":
							if err := openRecycleFolder(); err != nil {
								ui.StatusText = fmt.Sprintf("Open Recycle Bin failed: %v", err)
							} else {
								ui.StatusText = "Opened Recycle Bin"
							}
						case "Settings":
							OpenSettingsDialog(ui)
						case "Close":
							StopModFileWatcher(ui)
							CloseAllFileTabs(ui)
							if ui.ModTree != nil {
								ui.ModTree.Close()
								ui.ModTree = nil
								ui.ModulePath = ""
								ui.VisibleNodes = nil
								ui.Expanded = map[string]bool{"Data": true}
								ui.SelectedPath = ""
								ui.StatusText = "No module loaded"
								SyncWindowTitle(ui)
							}
						case "Associate .module files":
							if err := registerModuleAssociation(); err != nil {
								ui.StatusText = fmt.Sprintf("Associate failed: %v", err)
							} else {
								ui.StatusText = fmt.Sprintf("Associated .module with %s — double-click any .module to open", getExePath())
							}
						case "Remove .module association":
							if err := unregisterModuleAssociation(); err != nil {
								ui.StatusText = fmt.Sprintf("Remove failed: %v", err)
							} else {
								ui.StatusText = "Removed .module association (HKCU)"
							}
						case "Exit":
							exitLogged(0)
						default:
							ui.StatusText = fmt.Sprintf("Menu: %s", label)
						}
					} else if ui.MenuOpen == 1 {
						switch label {
						case "Save":
							if SaveEligible(ui) {
								HandleSaveActiveTab(ui)
							} else if tab := ActiveTab(ui); tab != nil && tab.Kind == "sgb" && tab.SgbMap != nil {
								HandleSgbSave(ui, tab)
							} else {
								ui.StatusText = "Save — no unsaved changes"
							}
						case "Export File":
							if eligible, reason := ExportEligible(ui, ActiveTab(ui)); eligible {
								HandleExportActiveTab(ui)
							} else {
								ui.StatusText = "Export — " + reason
							}
						case "Pack SGA...":
							PackCurrentModSGA(ui)
						case "Extract SGA...":
							OpenArchiveDialog(ui)
						default:
							ui.StatusText = fmt.Sprintf("Mod menu: %s", label)
						}
					} else if ui.MenuOpen == 2 {
						switch label {
						case "Open log folder":
							if err := openLogFolder(); err != nil {
								ui.StatusText = fmt.Sprintf("Open log folder failed: %v — log=%s", err, logDir())
							} else {
								ui.StatusText = fmt.Sprintf("Opened log folder: %s", logDir())
							}
						default:
							ui.StatusText = fmt.Sprintf("Menu: %s → %s", menus[ui.MenuOpen].Name, label)
						}
					} else {
						ui.StatusText = fmt.Sprintf("Menu: %s → %s", menus[ui.MenuOpen].Name, label)
					}
					ui.MenuOpen = -1
				}
				// The click handler closes the menu, so stop this loop before
				// the next iteration indexes menuItemClicks with menuOpen == -1.
				break
			}
		}
		if ui.DismissClick.Clicked(gtx) {
			ui.MenuOpen = -1
		}
	}
	// The language list, the search panel and the menus all hang off the menu
	// bar, and only one of them can own the space under it. Opening the list
	// puts the other two away when it happens, so this is the other half of
	// that rule, run after both of them have been resolved.
	CloseLocaleUnderOtherMenuBarPopups(ui)
	EnsureTabClicks(ui)
	for i := range ui.TabClicks {
		if ui.TabClicks[i].Clicked(gtx) {
			ui.TabSel = i
			MarkStateDirty(ui)
		}
	}
	for idx := range ui.TabCloses {
		if ui.TabCloses[idx].Clicked(gtx) {
			CloseTabAt(ui, idx)
			MarkStateDirty(ui)
			break
		}
	}
	if FdaActive(ui) {
		tab := ActiveTab(ui)
		CheckFdaAutoRepeat(ui)
		if tab.FdaPlayPause.Clicked(gtx) {
			FdaTogglePlayPause(ui)
		}
		if tab.FdaBack10.Clicked(gtx) {
			FdaSeekBy(ui, -10*time.Second)
		}
		if tab.FdaFwd10.Clicked(gtx) {
			FdaSeekBy(ui, 10*time.Second)
		}
		if tab.FdaBack30.Clicked(gtx) {
			FdaSeekBy(ui, -30*time.Second)
		}
		if tab.FdaFwd30.Clicked(gtx) {
			FdaSeekBy(ui, 30*time.Second)
		}
		if tab.FdaRepeatBtn.Clicked(gtx) {
			tab.FdaRepeat = !tab.FdaRepeat
			ui.StatusText = fmt.Sprintf("Repeat: %v", tab.FdaRepeat)
		}
		if tab.FdaExportBtn.Clicked(gtx) {
			FdaExportWav(ui)
		}
		if tab.FdaStopBtn.Clicked(gtx) {
			if tab.FdaPlayer != nil {
				tab.FdaPlayer.PauseAndStopReading()
				_, _ = tab.FdaPlayer.Seek(0, io.SeekStart)
				tab.FdaPaused = true
				ui.StatusText = "Stopped"
			}
		}
	}
	if tab := ActiveTab(ui); tab != nil && tab.Kind == "wav" && tab.WavPlayer != nil {
		CheckWavAutoRepeat(ui)
		if tab.WavPlayClick.Clicked(gtx) {
			WavTogglePlayPause(ui)
		}
		if tab.WavStopClick.Clicked(gtx) {
			WavStop(ui)
		}
		if tab.WavRepeatClick.Clicked(gtx) {
			tab.WavRepeat = !tab.WavRepeat
			ui.StatusText = fmt.Sprintf("WAV Repeat: %v", tab.WavRepeat)
		}
	}
	if tab := ActiveTab(ui); tab != nil && tab.Kind == "whm" {
		if tab.WhmConvertClick.Clicked(gtx) {
			ConvertWhmToSgmFile(ui, tab)
		}
	}
	if RgdActive(ui) {
		FocusClickedRgdEditor(ui, gtx)
		RgdSyncAndApply(ui, gtx)
	}
	if TextActive(ui) {
		TextSyncAndApply(ui, gtx)
	}
	nodes := GetVisibleNodes(ui)
	EnsureRowClicks(ui, len(nodes))
	EnsureRowHover(ui, len(nodes))
	HandleTreePointerInput(ui, gtx, nodes)
	if ui.ContextMenuVisible {
		if ui.ContextDuplicate.Clicked(gtx) {
			path := ui.ContextMenuPath
			CloseFileContextMenu(ui)
			if err := DuplicateCurrentModFile(ui, path); err != nil {
				ui.StatusText = fmt.Sprintf("Duplicate failed: %v", err)
			}
		} else if ui.ContextDelete.Clicked(gtx) && CanDeleteCurrentModFile(ui, ui.ContextMenuPath) {
			ui.FileDeletePath = ui.ContextMenuPath
			ui.FileDeleteDialog = true
			CloseFileContextMenu(ui)
		} else if ui.ContextDismissClick.Clicked(gtx) {
			CloseFileContextMenu(ui)
		}
	}
	if tab := ActiveTab(ui); tab != nil && tab.Kind == "rgd" {
		selBefore := tab.RgdSelected
		if len(tab.RgdMidNodes) == 0 {
			RebuildRgdMidNodesFor(ui, tab)
			EnsureRgdMidClicks(ui, tab, len(tab.RgdMidNodes))
		}
		midNodes := tab.RgdMidNodes
		EnsureRgdMidClicks(ui, tab, len(midNodes))
		clickedAny := false
		for i, m := range midNodes {
			if tab.RgdMidClicks[i].Clicked(gtx) {
				ui.KeyboardArea = "rgd"
				if m.IsTable && m.HasKids {
					if tab.RgdExpanded[m.Path] {
						delete(tab.RgdExpanded, m.Path)
					} else {
						tab.RgdExpanded[m.Path] = true
					}
				}
				if m.Path != selBefore {
					RgdFlushPending(ui, tab)
				}
				tab.RgdSelected = m.Path
				clickedAny = true
				MarkStateDirty(ui)
			}
		}
		if clickedAny {
			RebuildRgdMidNodesFor(ui, tab)
			EnsureRgdMidClicks(ui, tab, len(tab.RgdMidNodes))
		}
	}
}

func LayoutMenuBar(ui *uiState, gtx layout.Context) layout.Dimensions {
	h := gtx.Dp(unit.Dp(24))
	gtx.Constraints.Min.Y = h
	gtx.Constraints.Max.Y = h
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(gtx.Constraints.Max.X, h)
			fillRect(gtx, uikit.CMenuBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					var ch []layout.FlexChild
					for i, m := range menus {
						i, m := i, m
						ch = append(ch, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MenuButton(ui, gtx, i, m.Name) }))
					}
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, ch...)
				}),
			)
		}),
		// The object tools and the search field are flush with the right edge
		// of the window. They are placed by measuring the group and offsetting
		// it by whatever is left, instead of by a flexible gap inside the same
		// Flex: Gio sizes a Flexible child from the space left after all the
		// Rigid ones, so a gap written before them does not push them along,
		// and the field ends up past the window edge.
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			macro := op.Record(gtx.Ops)
			w := LayoutMenuRightGroup(ui, gtx).Size.X
			call := macro.Stop()
			if w > 0 && w < gtx.Constraints.Max.X {
				defer op.Offset(image.Pt(gtx.Constraints.Max.X-w, 0)).Push(gtx.Ops).Pop()
			}
			call.Add(gtx.Ops)
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
	)
}

// layoutMenuRightGroup is the right hand end of the menu bar: the language
// picker, the object interface tools and the search field, side by side.
func LayoutMenuRightGroup(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutLocaleSlot(ui, gtx) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutUnitPanelSlot(ui, gtx) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutSearchSlot(ui, gtx) }),
	)
}

func emptyRecycleBinMenuLabel() string {
	return fmt.Sprintf("Empty Recycle Bin (%d)", recycleFileCount())
}

func MenuItemsForOpen(ui *uiState) []string {
	if ui.MenuOpen != 0 {
		return menus[ui.MenuOpen].Items
	}
	items := append([]string(nil), menus[0].Items...)
	for i, item := range items {
		if item == "Empty Recycle Bin" {
			items[i] = emptyRecycleBinMenuLabel()
		}
	}
	if !ui.RecentMenuExpanded || len(ui.RecentModules) == 0 {
		return items
	}
	header := recentModulesHeaderIndex()
	if header < 0 {
		return items
	}
	tail := append([]string(nil), items[header+1:]...)
	items = append(items[:header+1], "—")
	for _, modulePath := range ui.RecentModules {
		items = append(items, filepath.Base(modulePath))
	}
	return append(items, tail...)
}

func LayoutMenuDropdown(ui *uiState, gtx layout.Context) layout.Dimensions {
	menuBarH := gtx.Dp(unit.Dp(24))
	xOff := gtx.Dp(unit.Dp(6))
	for i := 0; i < ui.MenuOpen; i++ {
		w := len(menus[i].Name)*7 + 20
		xOff += gtx.Dp(unit.Dp(w))
		xOff += gtx.Dp(unit.Dp(2))
	}
	items := MenuItemsForOpen(ui)
	EnsureMenuItemClicks(ui, ui.MenuOpen, len(items))
	itemH := gtx.Dp(unit.Dp(22))
	ddW := gtx.Dp(unit.Dp(220))
	ddH := len(items)*itemH + gtx.Dp(unit.Dp(6))
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return ui.DismissClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Max} })
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx2 := gtx
			defer op.Offset(image.Pt(xOff, menuBarH)).Push(gtx.Ops).Pop()
			sz := image.Pt(ddW, ddH)
			shadowOff := gtx.Dp(unit.Dp(2))
			shadowSz := image.Pt(ddW+shadowOff, ddH+shadowOff)
			st := clip.Rect{Max: shadowSz}.Push(gtx2.Ops)
			paint.ColorOp{Color: rgba(0, 0, 0, 28)}.Add(gtx2.Ops)
			paint.PaintOp{}.Add(gtx2.Ops)
			st.Pop()
			st2 := clip.Rect{Max: sz}.Push(gtx2.Ops)
			paint.ColorOp{Color: uikit.CPanelBg}.Add(gtx2.Ops)
			paint.PaintOp{}.Add(gtx2.Ops)
			st2.Pop()
			b := widget.Border{Color: uikit.CBorder, Width: unit.Dp(1)}
			gtx2.Constraints.Min = sz
			gtx2.Constraints.Max = sz
			return b.Layout(gtx2, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					var ch []layout.FlexChild
					for idx, it := range items {
						idx, it := idx, it
						if it == "—" {
							ch = append(ch, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(8), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									sz2 := image.Pt(gtx.Constraints.Max.X, 1)
									gtx.Constraints.Min.Y = 1
									gtx.Constraints.Max.Y = 1
									st := clip.Rect{Max: sz2}.Push(gtx.Ops)
									paint.ColorOp{Color: uikit.CHeaderLine}.Add(gtx.Ops)
									paint.PaintOp{}.Add(gtx.Ops)
									st.Pop()
									return layout.Dimensions{Size: sz2}
								})
							}))
						} else {
							ch = append(ch, layout.Rigid(func(gtx layout.Context) layout.Dimensions { return MenuItem(ui, gtx, idx, it) }))
						}
					}
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
				})
			})
		}),
	)
}

// openSearchResult activates a hit exactly as a double-click in the tree would,
// so a file opens in the same viewer no matter how it was reached.
func OpenSearchResult(ui *uiState, i int) {
	if i < 0 || i >= len(ui.SearchHits) {
		return
	}
	f := ui.SearchHits[i].File
	if f == nil {
		return
	}
	// Reveal it in the tree first, so the left panel and the open tab agree.
	RevealInTree(ui, f.FullPath)
	OpenTreeFile(ui, modtree.VisibleNode{
		Name:     f.Name,
		FullPath: f.FullPath,
		IsFile:   true,
		ModName:  f.Source.ModName,
		Kind:     f.Source.Kind,
	})
	// The query is kept so it can be refined, so the panel is held down for as
	// long as the field keeps that text.
	CloseSearch(ui)
}

// handleSearchInput owns the search field's keyboard. It must run before
// handleKeyboard, otherwise the arrow keys move the file tree instead of the
// result list.
func HandleSearchInput(ui *uiState, gtx layout.Context) {
	// Ctrl+F opens the field from anywhere, including while a text file is
	// being edited. It is the one shortcut worth stealing for this.
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.Name("F"), Required: key.ModShortcut})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		ui.SearchVisible = true
		ui.SearchHeldQuery = ""
		gtx.Execute(key.FocusCmd{Tag: &ui.SearchEditor})
		break
	}
	// Clicking the field focuses it, which is all that is needed for typing to
	// work: the editor is part of the field, not of the panel below it.
	if ui.SearchFieldClick.Clicked(gtx) {
		ui.SearchVisible = true
		ui.SearchHeldQuery = ""
		gtx.Execute(key.FocusCmd{Tag: &ui.SearchEditor})
	}

	// Escape has to be taken before the editor's own Update, or the editor
	// would use it to drop a text selection instead of closing the search.
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		e, ok := ev.(key.Event)
		if !ok || e.State != key.Press {
			continue
		}
		if strings.TrimSpace(ui.SearchEditor.Text()) != "" {
			ui.SearchEditor.SetText("")
			RunSearch(ui, true)
		} else {
			CloseSearch(ui)
		}
		return
	}

	// The editor is laid out with the field on every frame, so updating it here
	// is what actually delivers the typed characters.
	submitted := false
	for {
		ev, ok := ui.SearchEditor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.SubmitEvent:
			submitted = true
		case widget.ChangeEvent:
			// Deliberately does not lift the hold. The panel is reopened by
			// the query check below, which compares the text itself: a
			// ChangeEvent can arrive without the text having changed, and
			// letting one reopen the panel would put its dismiss catcher
			// back over the content and swallow every click on the file
			// tree.
		}
	}
	// Typing opens the panel, and emptying the field closes it again. A search
	// dismissed by a click outside stays closed for as long as its query is
	// kept, so the panel does not spring back on the next frame.
	if SearchPanelWanted(ui, ui.SearchEditor.Text()) {
		ui.SearchVisible = true
	}
	if !ui.SearchVisible {
		return
	}
	RunSearch(ui, false)

	if ui.SearchDismiss.Clicked(gtx) {
		CloseSearch(ui)
		return
	}
	for i := range ui.SearchKindClicks {
		if ui.SearchKindClicks[i].Clicked(gtx) && i < len(modtree.SearchKinds) {
			ui.SearchKind = modtree.SearchKinds[i]
			RunSearch(ui, true)
		}
	}
	for i := range ui.SearchHitClicks {
		if i < len(ui.SearchHits) && ui.SearchHitClicks[i].Clicked(gtx) {
			OpenSearchResult(ui, i)
			return
		}
	}
	if submitted {
		OpenSearchResult(ui, ui.SearchSel)
		return
	}
	HandleSearchArrows(ui, gtx)
}

func LayoutContent(ui *uiState, gtx layout.Context) layout.Dimensions {
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := gtx.Constraints.Max
			fillRect(gtx, uikit.CContentBg, sz)
			return layout.Dimensions{Size: sz}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				total := gtx.Constraints.Max.X
				EnsurePanelWidths(ui, total)
				// The object interface takes the whole content width: its
				// own right-hand panel is hidden on every section, and the
				// properties panel belongs to the plain modtree view below.
				if UnitTabOwnsLeftPanel(ui) {
					return LayoutUnitTabBody(ui, gtx)
				}
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(FixedPanel(ui, ui.LeftPanelWidth, func(gtx layout.Context) layout.Dimensions { return LayoutLeftPanel(ui, gtx) })),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutPanelSplitter(ui, gtx, 0, total) }),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return LayoutMiddlePanel(ui, gtx) }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions { return LayoutPanelSplitter(ui, gtx, 1, total) }),
					layout.Rigid(FixedPanel(ui, ui.RightPanelWidth, func(gtx layout.Context) layout.Dimensions { return LayoutRightPanel(ui, gtx) })),
				)
			})
		}),
	)
}

func RestorePersistedState(ui *uiState, state persistedState) bool {
	if strings.TrimSpace(state.ModulePath) == "" {
		return false
	}
	if _, err := os.Stat(state.ModulePath); err != nil {
		return false
	}
	LoadModule(ui, state.ModulePath)
	if ui.ModTree == nil || !samePath(ui.ModulePath, state.ModulePath) {
		return false
	}
	if len(state.Expanded) > 0 {
		ui.Expanded = make(map[string]bool, len(state.Expanded))
		for path, expanded := range state.Expanded {
			ui.Expanded[path] = expanded
		}
	}
	ui.SelectedTreePath = state.SelectedTreePath
	if ui.SelectedTreePath == "" {
		ui.SelectedTreePath = "Data"
	}
	ui.SelectedPath = state.SelectedPath
	// The Object window's pickers are restored by name once the catalog
	// exists, which is the first frame something asks it for. The section is
	// an index into a fixed list, so it can be put back straight away.
	ui.UnitPickRace = state.ObjectRace
	ui.UnitPickPath = state.ObjectUnit
	if state.ObjectSection > 0 && state.ObjectSection < len(unitSections) {
		ui.UnitSection = state.ObjectSection
	}
	for _, savedTab := range state.Tabs {
		if ui.ModTree.FindFile(savedTab.Path) == nil {
			continue
		}
		before := len(ui.OpenTabs)
		RestoreSavedTab(ui, savedTab)
		if len(ui.OpenTabs) > before && savedTab.RGDSelected != "" {
			ui.OpenTabs[len(ui.OpenTabs)-1].RgdSelected = savedTab.RGDSelected
			RebuildRgdMidNodesFor(ui, ui.OpenTabs[len(ui.OpenTabs)-1])
		}
	}
	ui.TabSel = state.ActiveTab
	EnsureTabClicks(ui)
	MarkStateDirty(ui)
	return true
}

func RestoreSavedTab(ui *uiState, saved persistedTab) {
	switch strings.ToLower(saved.Kind) {
	case "rgd":
		OpenRgd(ui, saved.Path)
	case "fda":
		OpenFda(ui, saved.Path)
	case "wav":
		OpenWav(ui, saved.Path)
	case "image":
		OpenImage(ui, saved.Path)
	case "whm":
		// A model tab has to go back through the model viewer, otherwise the
		// 3D preview is lost on every restart and the file opens as text.
		OpenWhm(ui, saved.Path)
	case "sgb":
		OpenSgb(ui, saved.Path)
	default:
		OpenTextFile(ui, saved.Path)
	}
}
