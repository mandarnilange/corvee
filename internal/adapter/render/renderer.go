package render

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Renderer implements domain.Renderer. All filesystem writes happen
// here — usecase.Render orchestrates and assembles the manifest, but
// every os.WriteFile / MkdirAll lives below this line per spec §S04.
type Renderer struct{}

// New returns a Renderer ready for use. The zero value is also valid;
// the constructor exists for symmetry with the other adapters.
func New() *Renderer { return &Renderer{} }

// descriptionPreviewMax caps the per-card description preview so wide
// blobs don't blow out the kanban card height. The HTML clamps with
// -webkit-line-clamp; this is the underlying source limit.
const descriptionPreviewMax = 240

// Render writes the dashboard's static HTML, theme, JS, manifest, and
// per-item detail pages under in.OutDir. Items are sorted by ID before
// rendering so output is deterministic across runs.
func (r *Renderer) Render(_ context.Context, in domain.RenderInput) (domain.RenderOutput, error) {
	if in.OutDir == "" {
		return domain.RenderOutput{}, fmt.Errorf("renderer: out_dir is required: %w", domain.ErrUsage)
	}
	if err := os.MkdirAll(filepath.Join(in.OutDir, "assets"), 0o755); err != nil {
		return domain.RenderOutput{}, fmt.Errorf("renderer: mkdir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(in.OutDir, "items"), 0o755); err != nil {
		return domain.RenderOutput{}, fmt.Errorf("renderer: mkdir items: %w", err)
	}

	items := append([]domain.Item(nil), in.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

	stats := computeStats(items)
	theme := resolveTheme(in.Theme)
	itemByID := indexByID(items)

	common := commonModel{
		WorkspaceName: in.WorkspaceName,
		Theme:         theme,
		Manifest:      in.Manifest,
	}

	board := buildBoardModel(common, items, itemByID)
	board.ActiveView = "board"
	board.RootPath = ""
	tree := buildTreeModel(common, items, itemByID)
	tree.ActiveView = "tree"
	tree.RootPath = ""
	summary := buildSummaryModel(common, items, itemByID, in.EventStats, in.Activity, in.CriticalPath)
	summary.ActiveView = "summary"
	summary.RootPath = ""

	written := make([]string, 0, 8+len(items))

	if err := r.executeTemplate("index.html", "templates/board.html.tmpl", board, in.OutDir); err != nil {
		return domain.RenderOutput{}, err
	}
	written = append(written, "index.html")

	if err := r.executeTemplate("tree.html", "templates/tree.html.tmpl", tree, in.OutDir); err != nil {
		return domain.RenderOutput{}, err
	}
	written = append(written, "tree.html")

	if err := r.executeTemplate("summary.html", "templates/summary.html.tmpl", summary, in.OutDir); err != nil {
		return domain.RenderOutput{}, err
	}
	written = append(written, "summary.html")

	// Per-item detail pages. Sorted iteration keeps output deterministic.
	for _, it := range items {
		page := buildItemModel(common, it, items, itemByID)
		dest := filepath.Join("items", it.ID+".html")
		if err := r.executeTemplate(dest, "templates/item.html.tmpl", page, in.OutDir); err != nil {
			return domain.RenderOutput{}, err
		}
		written = append(written, dest)
	}

	cssBytes, err := fs.ReadFile(themesFS, "themes/default.css")
	if err != nil {
		return domain.RenderOutput{}, fmt.Errorf("renderer: read stylesheet: %w", err)
	}
	if err := writeFile(filepath.Join(in.OutDir, "assets", "styles.css"), cssBytes); err != nil {
		return domain.RenderOutput{}, err
	}
	written = append(written, "assets/styles.css")

	jsBytes, err := fs.ReadFile(assetsFS, "assets/app.js")
	if err != nil {
		return domain.RenderOutput{}, fmt.Errorf("renderer: read app.js: %w", err)
	}
	if err := writeFile(filepath.Join(in.OutDir, "assets", "app.js"), jsBytes); err != nil {
		return domain.RenderOutput{}, err
	}
	written = append(written, "assets/app.js")

	manifestBytes, err := json.MarshalIndent(in.Manifest, "", "  ")
	if err != nil {
		return domain.RenderOutput{}, fmt.Errorf("renderer: marshal manifest: %w", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	if err := writeFile(filepath.Join(in.OutDir, "manifest.json"), manifestBytes); err != nil {
		return domain.RenderOutput{}, err
	}
	written = append(written, "manifest.json")

	return domain.RenderOutput{Files: written, Stats: stats}, nil
}

// executeTemplate parses the named template (plus the shared header
// partial) and writes the result to outDir/dest. Each call constructs
// a fresh *template.Template so recursive {{define}} blocks don't leak
// between views.
func (r *Renderer) executeTemplate(dest, src string, data any, outDir string) error {
	tmpl := template.New(filepath.Base(src))
	// Load the shared partials first so {{template "header"}} and
	// {{template "item-panel"}} resolve regardless of which view we're
	// rendering.
	for _, partial := range []string{
		"templates/_header.html.tmpl",
		"templates/_item_panel.html.tmpl",
		"templates/_drawer.html.tmpl",
		src,
	} {
		body, err := fs.ReadFile(templatesFS, partial)
		if err != nil {
			return fmt.Errorf("renderer: read template %s: %w", partial, err)
		}
		if _, err := tmpl.Parse(string(body)); err != nil {
			return fmt.Errorf("renderer: parse template %s: %w", partial, err)
		}
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("renderer: execute template %s: %w", src, err)
	}
	return writeFile(filepath.Join(outDir, dest), []byte(buf.String()))
}

// writeFile is a tiny wrapper that creates the parent directory and
// writes the bytes. Returns wrapped errors with the destination path
// for diagnostics.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("renderer: mkdir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("renderer: write %s: %w", path, err)
	}
	return nil
}

func indexByID(items []domain.Item) map[string]domain.Item {
	out := make(map[string]domain.Item, len(items))
	for _, it := range items {
		out[it.ID] = it
	}
	return out
}

// computeStats tallies items by structural type for the success envelope.
func computeStats(items []domain.Item) domain.RenderStats {
	var s domain.RenderStats
	for _, it := range items {
		switch it.Type {
		case domain.TypeProject:
			s.Projects++
		case domain.TypeEpic:
			s.Epics++
		case domain.TypeStory:
			s.Stories++
		case domain.TypeSubtask:
			s.Subtasks++
		}
	}
	return s
}

// itemDetailURL returns the dashboard-relative URL for an item's
// detail page. The path is stable so navigation works without a server.
func itemDetailURL(id string) string { return "items/" + id + ".html" }

// commonModel carries the fields every template needs from the
// shared _header partial.
type commonModel struct {
	WorkspaceName string
	Theme         string
	ActiveView    string // "board" | "tree" | "summary" | "item"
	RootPath      string // "" for root pages; "../" from items/<id>.html
	Manifest      domain.Manifest
}

// statusColumns is the ordered list of columns rendered on the board
// view. Order matches spec §5's lifecycle progression so eyes can scan
// left-to-right.
var statusColumns = []struct {
	Status domain.Status
	Label  string
}{
	{domain.StatusBacklog, "Backlog"},
	{domain.StatusReady, "Ready"},
	{domain.StatusClaimed, "Claimed"},
	{domain.StatusInProgress, "In Progress"},
	{domain.StatusReview, "Review"},
	{domain.StatusBlocked, "Blocked"},
	{domain.StatusDone, "Done"},
	{domain.StatusAbandoned, "Abandoned"},
}

// boardCard is the per-card payload for the board template.
type boardCard struct {
	ID              string
	Title           string
	Description     string
	TypePill        string
	Priority        string
	Status          string
	Kind            string
	Assignee        string
	DueDate         string
	DueOverdue      bool
	EstimatedHours  string
	DependencyCount int
	BlockCount      int
	AcceptanceCount int
	CapabilityCount int
	Tags            []string
	Breadcrumb      string
	DetailURL       string
	ProjectID       string
	SearchBlob      string // lowercased haystack for the search input

	// Internal sort key — items under the same epic group together.
	EpicKey   string
	EpicTitle string
}

// boardEpicGroup is a sub-grouping within a status column. Cards under
// the same immediate parent (epic or project) are grouped together so
// the board surfaces hierarchy without losing the kanban shape.
type boardEpicGroup struct {
	EpicID    string
	EpicTitle string
	Cards     []boardCard
}

// boardColumn is one column on the kanban board.
type boardColumn struct {
	Status domain.Status
	Label  string
	Count  int
	Groups []boardEpicGroup
}

// filterChip is one button on the board's filter bar. Count is the
// number of items currently matching the value so empty buckets can
// be hidden client-side.
type filterChip struct {
	Value string
	Label string
	Count int
}

// boardFilters carries the chip metadata for the filter bar.
type boardFilters struct {
	Statuses   []filterChip
	Priorities []filterChip
	Kinds      []filterChip
	Assignees  []filterChip
}

// boardModel is the data binding for board.html.tmpl.
type boardModel struct {
	commonModel
	Columns    []boardColumn
	Filters    boardFilters
	ItemPanels []itemView
}

func buildBoardModel(common commonModel, items []domain.Item, byID map[string]domain.Item) boardModel {
	byStatus := map[domain.Status][]boardCard{}
	for _, it := range items {
		card := buildBoardCard(it, byID)
		byStatus[it.Status] = append(byStatus[it.Status], card)
	}
	cols := make([]boardColumn, 0, len(statusColumns))
	for _, c := range statusColumns {
		cards := byStatus[c.Status]
		// Sort by epic key then ID for deterministic grouping.
		sort.SliceStable(cards, func(i, j int) bool {
			if cards[i].EpicKey != cards[j].EpicKey {
				return cards[i].EpicKey < cards[j].EpicKey
			}
			return cards[i].ID < cards[j].ID
		})
		groups := groupCardsByEpic(cards)
		cols = append(cols, boardColumn{
			Status: c.Status,
			Label:  c.Label,
			Count:  len(cards),
			Groups: groups,
		})
	}
	return boardModel{
		commonModel: common,
		Columns:     cols,
		Filters:     buildBoardFilters(items),
		ItemPanels:  buildAllItemViews(items, byID),
	}
}

// buildBoardFilters computes the chip set for the board's filter bar.
// Statuses with zero items in this workspace are still emitted so the
// list shape matches the kanban columns; consumers may hide them.
func buildBoardFilters(items []domain.Item) boardFilters {
	statusCount := map[domain.Status]int{}
	priorityCount := map[domain.Priority]int{}
	kindCount := map[domain.Kind]int{}
	assigneeCount := map[string]int{}
	for _, it := range items {
		statusCount[it.Status]++
		if it.Priority != "" {
			priorityCount[it.Priority]++
		}
		if it.Kind != "" {
			kindCount[it.Kind]++
		}
		if it.Claim != nil && it.Claim.Agent != "" {
			assigneeCount[it.Claim.Agent]++
		}
	}
	f := boardFilters{}
	for _, c := range statusColumns {
		if n := statusCount[c.Status]; n > 0 {
			f.Statuses = append(f.Statuses, filterChip{Value: string(c.Status), Label: c.Label, Count: n})
		}
	}
	for _, p := range []domain.Priority{domain.PriorityCritical, domain.PriorityHigh, domain.PriorityMedium, domain.PriorityLow} {
		if n := priorityCount[p]; n > 0 {
			f.Priorities = append(f.Priorities, filterChip{Value: string(p), Label: string(p), Count: n})
		}
	}
	for _, k := range []domain.Kind{domain.KindFeature, domain.KindBug, domain.KindChore, domain.KindSpike, domain.KindDocs, domain.KindRefactor} {
		if n := kindCount[k]; n > 0 {
			f.Kinds = append(f.Kinds, filterChip{Value: string(k), Label: string(k), Count: n})
		}
	}
	assignees := make([]string, 0, len(assigneeCount))
	for a := range assigneeCount {
		assignees = append(assignees, a)
	}
	sort.Strings(assignees)
	for _, a := range assignees {
		f.Assignees = append(f.Assignees, filterChip{Value: a, Label: a, Count: assigneeCount[a]})
	}
	return f
}

// buildAllItemViews returns the per-item panel models that the
// board/tree pages embed as <template> blocks for the drawer to
// render on click. urlPrefix is "" because these are embedded in a
// root-level page; deps/parent/project links resolve against the
// host page's location. Sorted by ID for deterministic output.
func buildAllItemViews(items []domain.Item, byID map[string]domain.Item) []itemView {
	out := make([]itemView, 0, len(items))
	for _, it := range items {
		out = append(out, buildItemView(it, items, byID, ""))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func buildBoardCard(it domain.Item, byID map[string]domain.Item) boardCard {
	card := boardCard{
		ID:              it.ID,
		Title:           it.Title,
		Description:     truncate(it.Description, descriptionPreviewMax),
		Priority:        string(it.Priority),
		Status:          string(it.Status),
		Kind:            string(it.Kind),
		Tags:            it.Tags,
		DependencyCount: len(it.Dependencies),
		BlockCount:      len(it.Blocks),
		AcceptanceCount: len(it.AcceptanceCriteria),
		CapabilityCount: len(it.RequiredCapabilities),
		DetailURL:       itemDetailURL(it.ID),
		ProjectID:       it.ProjectID,
	}
	// Type pill on non-project items only — project cards are already obvious.
	if it.Type != "" && it.Type != domain.TypeProject {
		card.TypePill = string(it.Type)
	}
	if it.Claim != nil {
		card.Assignee = it.Claim.Agent
	}
	if it.DueDate != nil {
		card.DueDate = it.DueDate.Format("Jan 2")
		card.DueOverdue = it.DueDate.Before(time.Now()) && it.Status != domain.StatusDone && it.Status != domain.StatusAbandoned
	}
	if it.Impact.EstimatedHours > 0 {
		card.EstimatedHours = fmt.Sprintf("%g", it.Impact.EstimatedHours)
	}
	parent, hasParent := byID[it.ParentID]
	if hasParent {
		card.EpicKey = parent.ID
		card.EpicTitle = parent.Title
	}
	// Breadcrumb: project / epic chain (exclude the item itself).
	card.Breadcrumb = breadcrumbString(it, byID)
	card.SearchBlob = strings.ToLower(strings.Join([]string{
		it.ID, it.Title, it.Description, strings.Join(it.Tags, " "),
		string(it.Kind), string(it.Priority),
	}, " "))
	return card
}

func groupCardsByEpic(cards []boardCard) []boardEpicGroup {
	if len(cards) == 0 {
		return nil
	}
	groups := make([]boardEpicGroup, 0)
	var current *boardEpicGroup
	for _, c := range cards {
		if current == nil || current.EpicID != c.EpicKey {
			groups = append(groups, boardEpicGroup{EpicID: c.EpicKey, EpicTitle: c.EpicTitle})
			current = &groups[len(groups)-1]
		}
		current.Cards = append(current.Cards, c)
	}
	return groups
}

// breadcrumbString returns "PRJ / PRJ-E01" style chain for the
// item — excluding the item itself.
func breadcrumbString(it domain.Item, byID map[string]domain.Item) string {
	if it.ParentID == "" {
		return ""
	}
	var chain []string
	cur := it.ParentID
	guard := 0
	for cur != "" && guard < 16 {
		chain = append([]string{cur}, chain...)
		parent, ok := byID[cur]
		if !ok {
			break
		}
		cur = parent.ParentID
		guard++
	}
	return strings.Join(chain, " / ")
}

func truncate(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	// Truncate at the last word boundary before limit for nicer breaks.
	cut := limit
	for cut > 0 && s[cut-1] != ' ' && s[cut-1] != '\n' {
		cut--
	}
	if cut == 0 {
		cut = limit
	}
	return strings.TrimRight(s[:cut], " \n") + "…"
}

// treeNodeView is the per-node payload for tree.html.tmpl.
type treeNodeView struct {
	ID            string
	Title         string
	Type          domain.ItemType
	Status        domain.Status
	Priority      string
	Assignee      string
	Children      []treeNodeView
	HasChildren   bool
	CompletionPct int
	Open          bool
	DetailURL     string
}

// treeModel is the data binding for tree.html.tmpl.
type treeModel struct {
	commonModel
	Roots      []treeNodeView
	ItemPanels []itemView
}

func buildTreeModel(common commonModel, items []domain.Item, byID map[string]domain.Item) treeModel {
	g := domain.NewGraph(items)
	rootIDs := g.Roots()
	sort.Strings(rootIDs)
	roots := make([]treeNodeView, 0, len(rootIDs))
	for _, id := range rootIDs {
		it, ok := g.Item(id)
		if !ok {
			continue
		}
		roots = append(roots, buildTreeNodeView(g, it))
	}
	return treeModel{commonModel: common, Roots: roots, ItemPanels: buildAllItemViews(items, byID)}
}

func buildTreeNodeView(g domain.Graph, it domain.Item) treeNodeView {
	node := treeNodeView{
		ID:        it.ID,
		Title:     it.Title,
		Type:      it.Type,
		Status:    it.Status,
		Priority:  string(it.Priority),
		Open:      it.Type == domain.TypeProject || it.Type == domain.TypeEpic,
		DetailURL: itemDetailURL(it.ID),
	}
	if it.Claim != nil {
		node.Assignee = it.Claim.Agent
	}
	childIDs := g.Children(it.ID)
	if len(childIDs) > 0 {
		node.HasChildren = true
		node.Children = make([]treeNodeView, 0, len(childIDs))
		var done, total int
		for _, cid := range childIDs {
			cit, ok := g.Item(cid)
			if !ok {
				continue
			}
			child := buildTreeNodeView(g, cit)
			node.Children = append(node.Children, child)
			total++
			if cit.Status == domain.StatusDone {
				done++
			}
		}
		if total > 0 {
			node.CompletionPct = int(float64(done) / float64(total) * 100)
		}
	}
	return node
}

// summaryHero is the top-of-page key metrics row.
type summaryHero struct {
	Total         int
	Done          int
	InProgress    int
	Claimed       int
	Review        int
	Backlog       int
	Ready         int
	Blocked       int
	Open          int // anything not in {done, abandoned}
	CompletionPct int
}

// segment is one slice of a per-project segmented progress bar.
type segment struct {
	Status   string
	Count    int
	WidthPct int
}

// summaryProject is the per-project rollup row.
type summaryProject struct {
	ID             string
	Title          string
	Total          int
	Done           int
	InProgress     int
	Blocked        int
	CompletionPct  int
	Segments       []segment
	SegmentSummary string
	DetailURL      string
}

// summaryEpic is the per-epic rollup row.
type summaryEpic struct {
	ID            string
	Title         string
	Total         int
	Done          int
	InProgress    int
	Blocked       int
	CompletionPct int
	DetailURL     string
}

// summaryBlocked is one entry in the blocked-task callout list.
type summaryBlocked struct {
	ID        string
	Title     string
	Note      string
	DetailURL string
}

// summaryActivity is one row in the activity feed with display-ready fields.
type summaryActivity struct {
	Timestamp    string
	Actor        string
	ActorInitial string
	ActorColor   string
	Label        string
	ItemID       string
	DetailURL    string
}

// summaryCritical is one step in the rendered critical-path stepper.
type summaryCritical struct {
	ID        string
	Title     string
	Status    string
	Found     bool
	DetailURL string
}

// summaryVelocity is the high-level velocity panel.
type summaryVelocity struct {
	CompletedEvents int
	DistinctActors  int
}

// summaryModel is the data binding for summary.html.tmpl.
type summaryModel struct {
	commonModel
	Hero         summaryHero
	Velocity     summaryVelocity
	Projects     []summaryProject
	Epics        []summaryEpic
	BlockedItems []summaryBlocked
	Activity     []summaryActivity
	CriticalPath []summaryCritical
}

func buildSummaryModel(common commonModel, items []domain.Item, byID map[string]domain.Item,
	stats domain.EventStats, activity []domain.ActivityEvent, criticalPath []domain.RenderCriticalPathStep,
) summaryModel {
	hero := summaryHero{Total: len(items)}

	byProject := map[string]*summaryProject{}
	projects := make([]summaryProject, 0)
	for _, it := range items {
		if it.Type == domain.TypeProject {
			projects = append(projects, summaryProject{
				ID:        it.ID,
				Title:     it.Title,
				DetailURL: itemDetailURL(it.ID),
			})
		}
	}
	for i := range projects {
		byProject[projects[i].ID] = &projects[i]
	}

	byEpic := map[string]*summaryEpic{}
	epics := make([]summaryEpic, 0)
	for _, it := range items {
		if it.Type == domain.TypeEpic {
			epics = append(epics, summaryEpic{
				ID:        it.ID,
				Title:     it.Title,
				DetailURL: itemDetailURL(it.ID),
			})
		}
	}
	for i := range epics {
		byEpic[epics[i].ID] = &epics[i]
	}

	statusCounts := map[domain.Status]map[string]int{}

	blocked := make([]summaryBlocked, 0)
	for _, it := range items {
		switch it.Status {
		case domain.StatusDone:
			hero.Done++
		case domain.StatusInProgress:
			hero.InProgress++
		case domain.StatusClaimed:
			hero.Claimed++
		case domain.StatusReview:
			hero.Review++
		case domain.StatusBacklog:
			hero.Backlog++
		case domain.StatusReady:
			hero.Ready++
		case domain.StatusBlocked:
			hero.Blocked++
		case domain.StatusAbandoned:
			// Abandoned items don't contribute to any hero bucket; they're
			// excluded from "Total - Done" via the Done branch above
			// remaining the only completion signal we track.
		}

		pid := it.ProjectID
		if pid == "" && it.Type == domain.TypeProject {
			pid = it.ID
		}
		if p := byProject[pid]; p != nil {
			p.Total++
			switch it.Status {
			case domain.StatusDone:
				p.Done++
			case domain.StatusInProgress:
				p.InProgress++
			case domain.StatusBlocked:
				p.Blocked++
			case domain.StatusBacklog, domain.StatusReady, domain.StatusClaimed,
				domain.StatusReview, domain.StatusAbandoned:
				// Other statuses contribute to Total only (already incremented above).
			}
			if statusCounts[it.Status] == nil {
				statusCounts[it.Status] = map[string]int{}
			}
			statusCounts[it.Status][pid]++
		}

		// Roll story/subtask counts up to the epic.
		epicID := findAncestorByType(it, byID, domain.TypeEpic)
		if epicID != "" {
			if e := byEpic[epicID]; e != nil {
				e.Total++
				switch it.Status {
				case domain.StatusDone:
					e.Done++
				case domain.StatusInProgress:
					e.InProgress++
				case domain.StatusBlocked:
					e.Blocked++
				case domain.StatusBacklog, domain.StatusReady, domain.StatusClaimed,
					domain.StatusReview, domain.StatusAbandoned:
					// Other statuses contribute to Total only.
				}
			}
		}

		if it.Status == domain.StatusBlocked {
			entry := summaryBlocked{ID: it.ID, Title: it.Title, DetailURL: itemDetailURL(it.ID)}
			if n := len(it.Journal); n > 0 {
				entry.Note = it.Journal[n-1].Note
			}
			blocked = append(blocked, entry)
		}
	}
	hero.Open = hero.Total - hero.Done
	if hero.Total > 0 {
		hero.CompletionPct = int(float64(hero.Done) / float64(hero.Total) * 100)
	}

	statusOrder := []domain.Status{
		domain.StatusDone, domain.StatusReview, domain.StatusInProgress,
		domain.StatusClaimed, domain.StatusReady, domain.StatusBlocked,
		domain.StatusBacklog, domain.StatusAbandoned,
	}
	for i := range projects {
		p := &projects[i]
		if p.Total > 0 {
			p.CompletionPct = int(float64(p.Done) / float64(p.Total) * 100)
		}
		segs := make([]segment, 0, len(statusOrder))
		var parts []string
		for _, s := range statusOrder {
			n := 0
			if statusCounts[s] != nil {
				n = statusCounts[s][p.ID]
			}
			if n == 0 {
				continue
			}
			width := 0
			if p.Total > 0 {
				width = int(float64(n) / float64(p.Total) * 100)
			}
			segs = append(segs, segment{Status: string(s), Count: n, WidthPct: width})
			parts = append(parts, fmt.Sprintf("%s %d", s, n))
		}
		p.Segments = segs
		p.SegmentSummary = strings.Join(parts, " · ")
	}
	for i := range epics {
		e := &epics[i]
		if e.Total > 0 {
			e.CompletionPct = int(float64(e.Done) / float64(e.Total) * 100)
		}
	}

	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
	sort.Slice(epics, func(i, j int) bool { return epics[i].ID < epics[j].ID })
	sort.Slice(blocked, func(i, j int) bool { return blocked[i].ID < blocked[j].ID })

	feed := make([]summaryActivity, 0, len(activity))
	for _, e := range activity {
		feed = append(feed, summaryActivity{
			Timestamp:    formatActivityTime(e.Timestamp),
			Actor:        e.Actor,
			ActorInitial: initialOf(e.Actor),
			ActorColor:   actorColor(e.Actor),
			Label:        friendlyEventLabel(e.Type),
			ItemID:       e.ItemID,
			DetailURL:    detailURLIfPresent(e.ItemID, byID),
		})
	}

	critical := make([]summaryCritical, 0, len(criticalPath))
	for _, step := range criticalPath {
		critical = append(critical, summaryCritical{
			ID:        step.ID,
			Title:     step.Title,
			Status:    string(step.Status),
			Found:     step.Found,
			DetailURL: detailURLIfPresent(step.ID, byID),
		})
	}

	return summaryModel{
		commonModel:  common,
		Hero:         hero,
		Velocity:     summaryVelocity{CompletedEvents: stats.CompletedCount, DistinctActors: stats.DistinctActors},
		Projects:     projects,
		Epics:        epics,
		BlockedItems: blocked,
		Activity:     feed,
		CriticalPath: critical,
	}
}

// findAncestorByType walks parents until it hits an item of typ.
// Returns the ancestor's ID or "" if none found.
func findAncestorByType(it domain.Item, byID map[string]domain.Item, typ domain.ItemType) string {
	if it.Type == typ {
		return it.ID
	}
	cur := it.ParentID
	guard := 0
	for cur != "" && guard < 16 {
		parent, ok := byID[cur]
		if !ok {
			return ""
		}
		if parent.Type == typ {
			return parent.ID
		}
		cur = parent.ParentID
		guard++
	}
	return ""
}

func detailURLIfPresent(id string, byID map[string]domain.Item) string {
	if id == "" {
		return ""
	}
	if _, ok := byID[id]; !ok {
		return ""
	}
	return itemDetailURL(id)
}

func formatActivityTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04")
}

func initialOf(actor string) string {
	if actor == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(actor)[0]))
}

// actorColor deterministically picks a hue based on the actor's name
// so avatar bubbles are stable across renders.
func actorColor(actor string) string {
	if actor == "" {
		return "#888"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(actor))
	hue := int(h.Sum32() % 360)
	return fmt.Sprintf("hsl(%d, 55%%, 55%%)", hue)
}

func friendlyEventLabel(typ string) string {
	switch typ {
	case "item_added":
		return "created"
	case "item_updated":
		return "updated"
	case "claimed":
		return "claimed"
	case "completed":
		return "completed"
	case "moved":
		return "moved"
	case "renamed":
		return "renamed"
	case "auto_reopened":
		return "auto-reopened"
	case "auto_closed":
		return "auto-closed"
	case "deleted_cascade":
		return "deleted (cascade)"
	case "released":
		return "released"
	default:
		return typ
	}
}

// breadcrumbEntry is one segment in the item-detail page's
// project / parent breadcrumb.
type breadcrumbEntry struct {
	ID        string
	URL       string
	IsCurrent bool
}

// depEntry is one row in the deps/blocks/children lists. URL is empty
// when the referenced item is not in the workspace (e.g., dangling
// reference); template renders it as missing.
type depEntry struct {
	ID     string
	Title  string
	Status string
	Found  bool
	URL    string
}

// journalView is one journal row formatted for display.
type journalView struct {
	Timestamp string
	Actor     string
	Event     string
	Note      string
}

// itemImpactView mirrors domain.Impact with display-friendly fields.
type itemImpactView struct {
	Risk  string
	Files []string
}

// itemView is the body of item.html.tmpl and the embedded drawer
// panels in board.html / tree.html. URL fields are pre-resolved with
// the correct relative prefix for the page that hosts the view.
type itemView struct {
	ID                   string
	Title                string
	Description          string
	Type                 string
	Kind                 string
	Status               string
	Priority             string
	Assignee             string
	ClaimedAt            string
	LeaseID              string
	LeaseExpiresAt       string
	CompletedAt          string
	CreatedBy            string
	CreatedAt            string
	UpdatedAt            string
	DueDate              string
	DueOverdue           bool
	EstimatedHours       string
	Version              int
	ProjectID            string
	ProjectURL           string
	ParentID             string
	ParentURL            string
	Aliases              []string
	Tags                 []string
	RequiredCapabilities []string
	AcceptanceCriteria   []string
	Deliverable          string
	Dependencies         []depEntry
	Blocks               []depEntry
	Children             []depEntry
	Journal              []journalView
	Impact               itemImpactView
	Breadcrumbs          []breadcrumbEntry
}

// itemModel is the data binding for item.html.tmpl.
type itemModel struct {
	commonModel
	Item itemView
}

// buildItemView builds the per-item display payload used by the
// standalone item.html page and the embedded drawer templates. The
// urlPrefix is prepended to every cross-item link the view exposes
// (deps, blocks, children, parent, project, breadcrumbs) — "" when
// the view lives at the dashboard root, "../" when it lives under
// items/.
func buildItemView(it domain.Item, allItems []domain.Item, byID map[string]domain.Item, urlPrefix string) itemView {
	v := itemView{
		ID:                   it.ID,
		Title:                it.Title,
		Description:          it.Description,
		Type:                 string(it.Type),
		Kind:                 string(it.Kind),
		Status:               string(it.Status),
		Priority:             string(it.Priority),
		CreatedBy:            it.CreatedBy,
		Version:              it.Version,
		ProjectID:            it.ProjectID,
		ParentID:             it.ParentID,
		Aliases:              it.Aliases,
		Tags:                 it.Tags,
		RequiredCapabilities: it.RequiredCapabilities,
		AcceptanceCriteria:   it.AcceptanceCriteria,
		Deliverable:          it.Deliverable,
	}
	if it.ProjectID != "" {
		if _, ok := byID[it.ProjectID]; ok {
			v.ProjectURL = urlPrefix + itemDetailURL(it.ProjectID)
		}
	}
	if it.ParentID != "" {
		if _, ok := byID[it.ParentID]; ok {
			v.ParentURL = urlPrefix + itemDetailURL(it.ParentID)
		}
	}
	if it.Claim != nil {
		v.Assignee = it.Claim.Agent
		v.LeaseID = it.Claim.LeaseID
		v.ClaimedAt = it.Claim.ClaimedAt.UTC().Format("2006-01-02 15:04 UTC")
		if !it.Claim.ExpiresAt.IsZero() {
			v.LeaseExpiresAt = it.Claim.ExpiresAt.UTC().Format("2006-01-02 15:04 UTC")
		}
	}
	if it.CompletedAt != nil {
		v.CompletedAt = it.CompletedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	if !it.CreatedAt.IsZero() {
		v.CreatedAt = it.CreatedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	if !it.UpdatedAt.IsZero() {
		v.UpdatedAt = it.UpdatedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	if it.DueDate != nil {
		v.DueDate = it.DueDate.UTC().Format("2006-01-02")
		v.DueOverdue = it.DueDate.Before(time.Now()) && it.Status != domain.StatusDone && it.Status != domain.StatusAbandoned
	}
	if it.Impact.EstimatedHours > 0 {
		v.EstimatedHours = fmt.Sprintf("%.1f", it.Impact.EstimatedHours)
	}
	v.Impact = itemImpactView{Risk: string(it.Impact.Risk), Files: it.Impact.Files}

	v.Dependencies = buildDepEntries(it.Dependencies, byID, urlPrefix)
	v.Blocks = buildDepEntries(it.Blocks, byID, urlPrefix)

	// Children are inferred from ParentID, not stored on the item.
	for _, child := range allItems {
		if child.ParentID == it.ID {
			v.Children = append(v.Children, depEntry{
				ID:     child.ID,
				Title:  child.Title,
				Status: string(child.Status),
				Found:  true,
				URL:    urlPrefix + itemDetailURL(child.ID),
			})
		}
	}
	sort.Slice(v.Children, func(i, j int) bool { return v.Children[i].ID < v.Children[j].ID })

	for _, je := range it.Journal {
		v.Journal = append(v.Journal, journalView{
			Timestamp: je.Timestamp.UTC().Format("2006-01-02 15:04"),
			Actor:     je.Actor,
			Event:     je.Event,
			Note:      je.Note,
		})
	}

	v.Breadcrumbs = buildBreadcrumbs(it, byID, urlPrefix)
	return v
}

func buildItemModel(common commonModel, it domain.Item, allItems []domain.Item, byID map[string]domain.Item) itemModel {
	common.ActiveView = "item"
	common.RootPath = "../"
	return itemModel{commonModel: common, Item: buildItemView(it, allItems, byID, "../")}
}

func buildDepEntries(ids []string, byID map[string]domain.Item, urlPrefix string) []depEntry {
	if len(ids) == 0 {
		return nil
	}
	out := make([]depEntry, 0, len(ids))
	for _, id := range ids {
		entry := depEntry{ID: id}
		if other, ok := byID[id]; ok {
			entry.Title = other.Title
			entry.Status = string(other.Status)
			entry.Found = true
			entry.URL = urlPrefix + itemDetailURL(id)
		}
		out = append(out, entry)
	}
	return out
}

func buildBreadcrumbs(it domain.Item, byID map[string]domain.Item, urlPrefix string) []breadcrumbEntry {
	var chain []breadcrumbEntry
	cur := it.ParentID
	guard := 0
	for cur != "" && guard < 16 {
		parent, ok := byID[cur]
		entry := breadcrumbEntry{ID: cur, URL: urlPrefix + itemDetailURL(cur)}
		if !ok {
			entry.URL = ""
		}
		_ = parent
		chain = append([]breadcrumbEntry{entry}, chain...)
		if !ok {
			break
		}
		cur = parent.ParentID
		guard++
	}
	chain = append(chain, breadcrumbEntry{ID: it.ID, IsCurrent: true})
	return chain
}
