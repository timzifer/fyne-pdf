package pdf

import (
	"cmp"
	"context"
	"image"
	"math"
	"slices"
	"time"
)

const (
	// maxRenderPixels caps the size of a page image rendered for the main
	// view (~100 MB RGBA). Beyond that the image is scaled up for display.
	maxRenderPixels = 25_000_000

	// renderBudgetPixels caps the page images kept for the main view
	// together (~200 MB RGBA). The pages in view share it; beyond that
	// they are rendered at a lower resolution. Images of pages out of view
	// are dropped, least recently shown first.
	renderBudgetPixels = 2 * maxRenderPixels
)

type (
	// renderJob asks for page at dpi.
	renderJob struct {
		page int
		dpi  float64
	}

	// renderedPage is a page image kept for the main view.
	renderedPage struct {
		img image.Image
		dpi float64
		// used orders the images by when they were last wanted.
		used uint64
	}
)

// adequate reports whether an image rendered at dpi have can be shown
// where want is needed: rendering at a lower resolution than shown is
// visible, a slightly higher one is not worth the time.
func adequate(have, want float64) bool {
	return have >= want && have <= want*1.5
}

// pagePixels is the number of pixels of a page of bound points at dpi.
func pagePixels(points float64, dpi float64) float64 {
	return points * (dpi / 72) * (dpi / 72)
}

// wantedRenders lists the renders the main view needs, see planRenders.
func (d *Document) wantedRenders() []renderJob {
	visible, live := d.visiblePages()
	d.mutex.Lock()
	bounds := d.bounds
	d.mutex.Unlock()
	if len(visible) == 0 || bounds == nil {
		return nil
	}
	return planRenders(visible, live, bounds, 96*d.scale*float64(d.canvasScale()))
}

// planRenders lists the renders for the pages visible and live (visible
// and their neighbours) at dpi, most important first: the visible pages in
// their order, then the neighbours as long as they fit into the budget.
// Where the visible pages alone exceed the budget, they are rendered at a
// lower resolution.
func planRenders(visible, live []int, bounds []image.Rectangle, dpi float64) []renderJob {
	points := func(page int) float64 {
		b := bounds[page]
		return float64(max(b.Dx(), 1) * max(b.Dy(), 1))
	}
	dpiFor := func(page int) float64 {
		return math.Min(dpi, 72*math.Sqrt(maxRenderPixels/points(page)))
	}

	jobs := make([]renderJob, 0, len(live))
	var total float64
	for _, page := range visible {
		dpi := dpiFor(page)
		jobs = append(jobs, renderJob{page, dpi})
		total += pagePixels(points(page), dpi)
	}
	if total > renderBudgetPixels {
		factor := math.Sqrt(renderBudgetPixels / total)
		for i := range jobs {
			jobs[i].dpi *= factor
		}
		total = renderBudgetPixels
	}
	for _, page := range live {
		if slices.Contains(visible, page) {
			continue
		}
		dpi := dpiFor(page)
		if px := pagePixels(points(page), dpi); total+px <= renderBudgetPixels {
			jobs = append(jobs, renderJob{page, dpi})
			total += px
		}
	}
	return jobs
}

// scheduleRender renders the pages the main view needs after delay, unless
// they are rendered at a fitting resolution already. A later call replaces
// an earlier one that has not started yet; a render in progress is
// cancelled if its page is no longer needed at that resolution.
func (d *Document) scheduleRender(delay time.Duration) {
	jobs := d.wantedRenders()

	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.source == nil {
		return
	}
	if d.renderTimer != nil && d.renderTimer.Stop() {
		d.pending.Done()
	}
	src, ctx := d.source, d.ctx
	d.pending.Add(1)
	d.renderTimer = time.AfterFunc(delay, func() {
		defer d.pending.Done()
		if d.startRenders(ctx, jobs) {
			d.renderLoop(src, ctx)
		}
	})
}

// startRenders makes jobs the wanted renders. It reports whether the
// caller has to render them; otherwise a render loop is running already.
func (d *Document) startRenders(ctx context.Context, jobs []renderJob) bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if ctx != d.ctx || ctx.Err() != nil {
		return false
	}
	d.jobs = jobs
	for _, job := range jobs {
		if r, ok := d.rendered[job.page]; ok {
			d.useClock++
			r.used = d.useClock
		}
	}
	if !d.rendering {
		d.rendering = true
		return true
	}
	if d.inflightCancel != nil && !slices.ContainsFunc(jobs, func(j renderJob) bool {
		return j.page == d.inflight.page && adequate(d.inflight.dpi, j.dpi)
	}) {
		d.inflightCancel()
	}
	return false
}

// renderLoop renders the wanted pages one after another until all are
// rendered, or ctx is cancelled.
func (d *Document) renderLoop(src *Source, ctx context.Context) {
	for {
		d.mutex.Lock()
		job, ok := d.nextJobLocked(ctx)
		if !ok {
			if ctx == d.ctx {
				d.rendering = false
			}
			d.mutex.Unlock()
			return
		}
		jobCtx, cancel := context.WithCancel(ctx)
		d.inflight, d.inflightCancel = job, cancel
		d.mutex.Unlock()

		img, err := src.RenderPageContext(jobCtx, job.page, job.dpi)

		d.mutex.Lock()
		cancelled := jobCtx.Err() != nil
		cancel()
		if cancelled {
			// Cancelled: either the document is gone (the next pass
			// stops) or the page is no longer needed.
			if ctx == d.ctx {
				d.inflightCancel = nil
			}
			d.mutex.Unlock()
			continue
		}
		d.inflightCancel = nil
		if err != nil {
			d.failed[job.page] = job.dpi
		} else {
			d.storeLocked(job.page, img, job.dpi)
		}
		d.mutex.Unlock()

		d.runOnMain(func() {
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				d.reportError(job.page, err)
			}
			d.showImages()
		})
	}
}

// nextJobLocked returns the most important wanted render that is not done
// yet, if ctx is still the document's.
func (d *Document) nextJobLocked(ctx context.Context) (renderJob, bool) {
	if ctx != d.ctx || ctx.Err() != nil {
		return renderJob{}, false
	}
	for _, job := range d.jobs {
		if r, ok := d.rendered[job.page]; ok && adequate(r.dpi, job.dpi) {
			continue
		}
		if dpi, ok := d.failed[job.page]; ok && adequate(dpi, job.dpi) {
			continue
		}
		return job, true
	}
	return renderJob{}, false
}

// storeLocked keeps img for page and drops the least recently used images
// of pages no longer wanted while the budget is exceeded.
func (d *Document) storeLocked(page int, img image.Image, dpi float64) {
	if old, ok := d.rendered[page]; ok {
		d.renderedPixels -= imagePixels(old.img)
	}
	d.useClock++
	d.rendered[page] = &renderedPage{img: img, dpi: dpi, used: d.useClock}
	d.renderedPixels += imagePixels(img)
	delete(d.failed, page)

	for d.renderedPixels > renderBudgetPixels {
		var victims []int
		for p := range d.rendered {
			if !slices.ContainsFunc(d.jobs, func(j renderJob) bool { return j.page == p }) {
				victims = append(victims, p)
			}
		}
		if len(victims) == 0 {
			return
		}
		victim := slices.MinFunc(victims, func(a, b int) int {
			return cmp.Compare(d.rendered[a].used, d.rendered[b].used)
		})
		d.renderedPixels -= imagePixels(d.rendered[victim].img)
		delete(d.rendered, victim)
	}
}

func imagePixels(img image.Image) int {
	return img.Bounds().Dx() * img.Bounds().Dy()
}

// showImages shows the best image there is for each page in the view: its
// render, or its thumbnail until it is rendered.
func (d *Document) showImages() {
	d.mutex.Lock()
	images := make(map[int]image.Image, len(d.views))
	for page := range d.views {
		if r, ok := d.rendered[page]; ok {
			images[page] = r.img
		}
	}
	d.mutex.Unlock()

	for page, view := range d.views {
		img, ok := images[page]
		if !ok {
			img = d.placeholder(page)
		}
		if view.image.Image != img {
			view.SetImage(img)
		}
	}
}

// placeholder returns the thumbnail of page, or an empty image.
func (d *Document) placeholder(page int) image.Image {
	if page < len(d.thumbnails) {
		return d.thumbnails[page].page.image.Image
	}
	return emptyImage
}

// waitRendered blocks until all scheduled renders are done.
func (d *Document) waitRendered() {
	d.pending.Wait()
}
