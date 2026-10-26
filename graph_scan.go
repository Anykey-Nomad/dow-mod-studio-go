package main

// Reading the links in the background.
//
// The graph indexes every .rgd of Data\attrib from the folder layout alone,
// which is cheap. Finding the references inside the three thousand-odd
// referring files is not: measured at around thirty seconds for the DXP3 tree.
// Paying that on the frame the user opens the relationships view would freeze
// the window for as long as it took, so it is done the way the unit names and
// the squad index are done — off the UI thread, in the background, with
// whatever has arrived so far shown while the rest is still coming.
//
// The scan is started when the relationships view is opened rather than when
// the module is, because a session that never looks at them should not pay for
// them at all.

// graphLinkBatch is a slice of the scan's results. done marks the last one.
type graphLinkBatch struct {
	links []graphLink
	done  bool
}

// graphLinkBatchSize is how many links one batch carries. The links of one
// unit arrive together, so a unit is never split across two batches.
const graphLinkBatchSize = 128

// startGraphLinkScan reads every unit's and building's references in the
// background, into a fresh link list on the graph.
//
// A scan already running for the same tree is left alone, and a scan for a
// different tree is stopped rather than left writing into a graph nobody reads
// any more.
func (ui *uiState) startGraphLinkScan() *modGraph {
	g := ui.graphFor()
	if g == nil {
		return nil
	}
	if ui.graphLinksRunning && ui.graphLinksGraph == g {
		return g
	}
	ui.stopGraphLinkScan()

	ui.graphLinksGraph = g
	g.BeginLinkScan()
	ui.graphLinksCh = make(chan graphLinkBatch, 8)
	stop := make(chan struct{})
	ui.graphLinksStop = stop
	ui.graphLinksRunning = true

	// The entities are copied so the goroutine does not walk a graph the UI
	// thread is free to replace.
	entities := append([]*graphEntity(nil), g.EntitiesOfKind(graphLinkKinds()...)...)
	ch := ui.graphLinksCh
	go func() {
		batch := make([]graphLink, 0, graphLinkBatchSize)
		flush := func(done bool) bool {
			out := make([]graphLink, len(batch))
			copy(out, batch)
			select {
			case ch <- graphLinkBatch{links: out, done: done}:
				batch = batch[:0]
				return true
			case <-stop:
				return false
			}
		}
		for _, e := range entities {
			for _, l := range g.ScanEntityLinks(e) {
				batch = append(batch, l)
			}
			if len(batch) >= graphLinkBatchSize && !flush(false) {
				return
			}
		}
		flush(true)
	}()
	return g
}

// stopGraphLinkScan ends a running scan. The goroutine notices through its
// stop channel and returns, so a module reload does not leave one reading a
// tree that is being closed underneath it.
func (ui *uiState) stopGraphLinkScan() {
	if ui.graphLinksStop != nil {
		close(ui.graphLinksStop)
		ui.graphLinksStop = nil
	}
	ui.graphLinksRunning = false
	ui.graphLinksCh = nil
}

// pollGraphLinks takes whatever the scan has finished and reports whether
// anything new arrived, so the caller can ask for a redraw.
func (ui *uiState) pollGraphLinks() bool {
	if ui.graphLinksCh == nil {
		return false
	}
	got := false
	for {
		select {
		case batch := <-ui.graphLinksCh:
			if ui.graphLinksGraph != nil {
				ui.graphLinksGraph.AddScannedLinks(batch.links)
			}
			if batch.done {
				if ui.graphLinksGraph != nil {
					ui.graphLinksGraph.FinishLinkScan()
				}
				ui.stopGraphLinkScan()
			}
			got = true
		default:
			return got
		}
	}
}
