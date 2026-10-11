package main

import (
	"fmt"
	"path/filepath"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func (ui *uiState) fdaPropertiesTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.fdaFile == nil && t.fdaError == "" {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), "No FDA selected")
			l.Color = cTextDim
			return l.Layout(gtx)
		})
	}
	if t.fdaError != "" {
		return layout.Inset{Top: unit.Dp(8), Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(ui.th, unit.Sp(10), t.fdaError)
			l.Color = rgb(0xB00020)
			return l.Layout(gtx)
		})
	}
	info := t.fdaFile.Info
	bitrateK := float64(info.BlockBitrate) * 86.1328125 * float64(info.Channels) / 1000.0
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableHead(gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "File", filepath.Base(t.path), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Channels", fmt.Sprintf("%d", info.Channels), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Sample Rate", fmt.Sprintf("%d Hz", info.SampleRate), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Bits", fmt.Sprintf("%d", info.SampleSize), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Block Bitrate", fmt.Sprintf("%d", info.BlockBitrate), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Bitrate", fmt.Sprintf("%.1f kbps", bitrateK), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Duration", fmtDuration(t.fdaTotal), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Position", fmtDuration(ui.fdaPosFor(t)), true)
		}),
	)
}

func (ui *uiState) fdaInfoTable(gtx layout.Context, t *fileTab) layout.Dimensions {
	if t.fdaFile == nil {
		return layout.Dimensions{}
	}
	fb := t.fdaFile.FBIF
	hdr := t.fdaFile.Header
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableHead(gtx, "Name", "Value") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Version", fmt.Sprintf("%d.%d", hdr.MajorVer, hdr.MinorVer), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableRow2(gtx, "Plugin", fb.PluginName, true) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Plugin Ver", fmt.Sprintf("%d", fb.PluginVer), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableRow2(gtx, "User", fb.UserName, true) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return ui.tableRow2(gtx, "Burn Time", fb.BurnTime, false) }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "BeginLoop", fmt.Sprintf("%d", t.fdaFile.Info.BeginLoop), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "EndLoop", fmt.Sprintf("%d", t.fdaFile.Info.EndLoop), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "StartOffset", fmt.Sprintf("%d", t.fdaFile.Info.StartOffset), true)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Raw Data", fmt.Sprintf("%d bytes", len(t.fdaFile.RawData)), false)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.tableRow2(gtx, "Decoded", fmt.Sprintf("%d bytes", len(t.fdaPcmBytes)), true)
		}),
	)
}
