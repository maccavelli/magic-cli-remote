package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

const (
	maxNativeThreadPage        = 100
	maxThreadFallbackPages     = 20
	maxThreadFallbackRows      = 1000
	maxLoadedThreadPages       = 20
	maxThreadSections          = 1000
	maxNativeThreadHistoryPage = 100
)

type nativeRPCSender func(context.Context, string, any) (json.RawMessage, error)
type nativeCapability func(CapabilityID) bool

type nativeThreadAPI struct {
	send     nativeRPCSender
	supports nativeCapability
}

func newNativeThreadAPI(send nativeRPCSender, supports nativeCapability) *nativeThreadAPI {
	return &nativeThreadAPI{send: send, supports: supports}
}

type wireThreadSection struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Appearance *struct {
		Icon  string `json:"icon"`
		Color string `json:"color"`
	} `json:"appearance"`
}

type wireThread struct {
	ID             string             `json:"id"`
	CWD            string             `json:"cwd"`
	Name           *string            `json:"name"`
	Preview        string             `json:"preview"`
	Status         json.RawMessage    `json:"status"`
	Source         json.RawMessage    `json:"source"`
	CreatedAt      int64              `json:"createdAt"`
	UpdatedAt      int64              `json:"updatedAt"`
	RecencyAt      *int64             `json:"recencyAt"`
	ParentThreadID *string            `json:"parentThreadId"`
	ForkedFromID   *string            `json:"forkedFromId"`
	ProjectID      *string            `json:"projectId"`
	Section        *wireThreadSection `json:"section"`
	Turns          []wireTurn         `json:"turns"`
}

type wireTurn struct {
	ID    string            `json:"id"`
	Items []json.RawMessage `json:"items"`
}

type wireThreadPage struct {
	Data            []wireThread `json:"data"`
	NextCursor      *string      `json:"nextCursor"`
	BackwardsCursor *string      `json:"backwardsCursor"`
}

func boundedThreadLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > maxNativeThreadPage {
		return maxNativeThreadPage
	}
	return limit
}

func decodeTaggedValue(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return ""
	}
	for _, key := range []string{"type", "kind", "source"} {
		if json.Unmarshal(object[key], &text) == nil && text != "" {
			return text
		}
	}
	if len(object) == 1 {
		for key := range object {
			return key
		}
	}
	return ""
}

func projectWireThread(thread wireThread, archived, loaded bool) provider.AgentSessionMeta {
	updated := thread.UpdatedAt
	if thread.RecencyAt != nil && *thread.RecencyAt > updated {
		updated = *thread.RecencyAt
	}
	meta := provider.AgentSessionMeta{
		ID: thread.ID, CWD: thread.CWD, Preview: boundedPermissionText(thread.Preview, 1024),
		CreatedAt: unixTime(thread.CreatedAt), UpdatedAt: unixTime(updated),
		NativeStatus: decodeTaggedValue(thread.Status), Source: decodeTaggedValue(thread.Source),
		Archived: archived, Loaded: loaded,
	}
	if thread.Name != nil {
		meta.Title = boundedPermissionText(*thread.Name, 256)
	}
	if thread.ParentThreadID != nil {
		meta.ParentThreadID = *thread.ParentThreadID
	}
	if thread.ForkedFromID != nil {
		meta.ForkedFromID = *thread.ForkedFromID
	}
	if thread.ProjectID != nil {
		meta.ProjectID = *thread.ProjectID
	}
	if thread.Section != nil {
		meta.SectionID = thread.Section.ID
		meta.SectionName = boundedPermissionText(thread.Section.Name, 256)
		meta.Pinned = strings.EqualFold(thread.Section.ID, "pinned") || strings.EqualFold(thread.Section.Name, "pinned")
		if thread.Section.Appearance != nil {
			meta.SectionIcon = boundedPermissionText(thread.Section.Appearance.Icon, 64)
			meta.SectionColor = boundedPermissionText(thread.Section.Appearance.Color, 64)
		}
	}
	return meta
}

func unixTime(seconds int64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0).UTC()
}

func (a *nativeThreadAPI) loadedIDs(ctx context.Context) (map[string]bool, error) {
	loaded := make(map[string]bool)
	if a.supports == nil || !a.supports(CapabilityThreadLoadedList) {
		return loaded, nil
	}
	cursor := ""
	for page := 0; page < maxLoadedThreadPages; page++ {
		params := map[string]any{"limit": uint32(maxNativeThreadPage)}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := a.send(ctx, "thread/loaded/list", params)
		if err != nil {
			return loaded, err
		}
		var response struct {
			Data       []string `json:"data"`
			NextCursor *string  `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			return loaded, err
		}
		for _, id := range response.Data {
			if id != "" {
				loaded[id] = true
			}
		}
		if response.NextCursor == nil || *response.NextCursor == "" || *response.NextCursor == cursor {
			break
		}
		cursor = *response.NextCursor
	}
	return loaded, nil
}

func (a *nativeThreadAPI) List(ctx context.Context, opts provider.ThreadListOptions) (provider.NativeThreadPage, error) {
	limit := boundedThreadLimit(opts.Limit)
	params := map[string]any{"limit": uint32(limit)}
	if opts.Cursor != "" {
		params["cursor"] = opts.Cursor
	}
	if opts.Archived {
		params["archived"] = true
	}
	if opts.ParentThreadID != "" {
		params["parentThreadId"] = opts.ParentThreadID
	}
	if opts.AncestorThreadID != "" {
		params["ancestorThreadId"] = opts.AncestorThreadID
	}
	if opts.SectionID != "" {
		params["sectionId"] = opts.SectionID
	}
	if opts.ProjectID != "" {
		params["projectId"] = opts.ProjectID
	}
	if opts.SearchTerm != "" {
		params["searchTerm"] = opts.SearchTerm
	}
	raw, err := a.send(ctx, "thread/list", params)
	if err != nil {
		return provider.NativeThreadPage{}, err
	}
	var response wireThreadPage
	if err := json.Unmarshal(raw, &response); err != nil {
		return provider.NativeThreadPage{}, err
	}
	if len(response.Data) > maxNativeThreadPage {
		return provider.NativeThreadPage{}, errors.New("thread page exceeds bound")
	}
	loaded, err := a.loadedIDs(ctx)
	if err != nil {
		return provider.NativeThreadPage{}, err
	}
	page := provider.NativeThreadPage{Source: provider.ThreadSourceNative, Limit: limit}
	if response.NextCursor != nil {
		page.NextCursor = boundedPermissionText(*response.NextCursor, 1024)
	}
	if response.BackwardsCursor != nil {
		page.BackwardsCursor = boundedPermissionText(*response.BackwardsCursor, 1024)
	}
	for _, thread := range response.Data {
		if thread.ID != "" {
			page.Threads = append(page.Threads, projectWireThread(thread, opts.Archived, loaded[thread.ID]))
		}
	}
	return page, nil
}

func (a *nativeThreadAPI) Search(ctx context.Context, opts provider.ThreadSearchOptions) (provider.NativeThreadPage, error) {
	term := strings.TrimSpace(opts.Term)
	limit := boundedThreadLimit(opts.Limit)
	if term == "" {
		return provider.NativeThreadPage{Source: provider.ThreadSourceStableFallback, Limit: limit}, nil
	}
	if a.supports != nil && a.supports(CapabilityThreadSearch) {
		params := map[string]any{"searchTerm": term, "limit": uint32(limit)}
		if opts.Cursor != "" {
			params["cursor"] = opts.Cursor
		}
		if opts.Archived {
			params["archived"] = true
		}
		raw, err := a.send(ctx, "thread/search", params)
		if err == nil {
			var response struct {
				Data []struct {
					Thread wireThread `json:"thread"`
				} `json:"data"`
				NextCursor      *string `json:"nextCursor"`
				BackwardsCursor *string `json:"backwardsCursor"`
			}
			if err := json.Unmarshal(raw, &response); err != nil {
				return provider.NativeThreadPage{}, err
			}
			if len(response.Data) > maxNativeThreadPage {
				return provider.NativeThreadPage{}, errors.New("thread search page exceeds bound")
			}
			page := provider.NativeThreadPage{Source: provider.ThreadSourceNativeSearch, Limit: limit}
			if response.NextCursor != nil {
				page.NextCursor = boundedPermissionText(*response.NextCursor, 1024)
			}
			if response.BackwardsCursor != nil {
				page.BackwardsCursor = boundedPermissionText(*response.BackwardsCursor, 1024)
			}
			loaded, _ := a.loadedIDs(ctx)
			for _, row := range response.Data {
				if row.Thread.ID != "" {
					page.Threads = append(page.Threads, projectWireThread(row.Thread, opts.Archived, loaded[row.Thread.ID]))
				}
			}
			return page, nil
		}
	}

	// Stable fallback follows at most 20 pages/1000 rows and filters locally.
	// It is intentionally labelled and marked truncated: even a nil cursor
	// cannot prove an older installed binary did not omit scan-only rows.
	result := provider.NativeThreadPage{Source: provider.ThreadSourceStableFallback, Limit: limit, Truncated: true}
	cursor := ""
	needle := strings.ToLower(term)
	seen := 0
	for pageNo := 0; pageNo < maxThreadFallbackPages && seen < maxThreadFallbackRows; pageNo++ {
		page, err := a.List(ctx, provider.ThreadListOptions{Cursor: cursor, Limit: maxNativeThreadPage, Archived: opts.Archived})
		if err != nil {
			return provider.NativeThreadPage{}, err
		}
		seen += len(page.Threads)
		for _, thread := range page.Threads {
			haystack := strings.ToLower(thread.Title + "\n" + thread.Preview)
			if strings.Contains(haystack, needle) && len(result.Threads) < limit {
				result.Threads = append(result.Threads, thread)
			}
		}
		if page.NextCursor == "" || page.NextCursor == cursor || len(result.Threads) >= limit {
			break
		}
		cursor = page.NextCursor
	}
	return result, nil
}

func boundedNativeHistoryLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > maxNativeThreadHistoryPage {
		return maxNativeThreadHistoryPage
	}
	return limit
}

func nativeHistoryParams(opts provider.NativeThreadHistoryOptions) (map[string]any, int, error) {
	if strings.TrimSpace(opts.ThreadID) == "" {
		return nil, 0, errors.New("thread id is required")
	}
	limit := boundedNativeHistoryLimit(opts.Limit)
	params := map[string]any{"threadId": opts.ThreadID, "limit": uint32(limit)}
	if opts.TurnID != "" {
		params["turnId"] = opts.TurnID
	}
	if opts.Cursor != "" {
		params["cursor"] = opts.Cursor
	}
	if opts.SortDirection != "" {
		if opts.SortDirection != "asc" && opts.SortDirection != "desc" {
			return nil, 0, errors.New("sort direction must be asc or desc")
		}
		params["sortDirection"] = opts.SortDirection
	}
	return params, limit, nil
}

func decodeNativeHistoryPage(raw json.RawMessage, source string, limit int) (provider.NativeThreadHistoryPage, error) {
	var response struct {
		Data            []json.RawMessage `json:"data"`
		NextCursor      *string           `json:"nextCursor"`
		BackwardsCursor *string           `json:"backwardsCursor"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return provider.NativeThreadHistoryPage{}, err
	}
	if len(response.Data) > maxNativeThreadHistoryPage {
		return provider.NativeThreadHistoryPage{}, errors.New("native history page exceeds bound")
	}
	page := provider.NativeThreadHistoryPage{Data: response.Data, Source: source, Limit: limit}
	if response.NextCursor != nil {
		page.NextCursor = boundedPermissionText(*response.NextCursor, 1024)
	}
	if response.BackwardsCursor != nil {
		page.BackwardsCursor = boundedPermissionText(*response.BackwardsCursor, 1024)
	}
	return page, nil
}

func (a *nativeThreadAPI) ListTurns(ctx context.Context, opts provider.NativeThreadHistoryOptions) (provider.NativeThreadHistoryPage, error) {
	if a.supports == nil || !a.supports(CapabilityThreadTurnsList) {
		return provider.NativeThreadHistoryPage{}, errors.New("native turn pagination is unavailable")
	}
	params, limit, err := nativeHistoryParams(opts)
	if err != nil {
		return provider.NativeThreadHistoryPage{}, err
	}
	if opts.ItemsView != "" {
		if opts.ItemsView != "notLoaded" && opts.ItemsView != "summary" && opts.ItemsView != "full" {
			return provider.NativeThreadHistoryPage{}, errors.New("items view must be notLoaded, summary, or full")
		}
		params["itemsView"] = opts.ItemsView
	}
	delete(params, "turnId")
	raw, err := a.send(ctx, "thread/turns/list", params)
	if err != nil {
		return provider.NativeThreadHistoryPage{}, err
	}
	return decodeNativeHistoryPage(raw, provider.ThreadSourceNativeTurns, limit)
}

func (a *nativeThreadAPI) ListItems(ctx context.Context, opts provider.NativeThreadHistoryOptions) (provider.NativeThreadHistoryPage, error) {
	if a.supports == nil || !a.supports(CapabilityThreadItemsList) {
		return provider.NativeThreadHistoryPage{}, errors.New("native item pagination is unavailable")
	}
	params, limit, err := nativeHistoryParams(opts)
	if err != nil {
		return provider.NativeThreadHistoryPage{}, err
	}
	raw, err := a.send(ctx, "thread/items/list", params)
	if err != nil {
		return provider.NativeThreadHistoryPage{}, err
	}
	return decodeNativeHistoryPage(raw, provider.ThreadSourceNativeItems, limit)
}

func (a *nativeThreadAPI) Read(ctx context.Context, id string, includeTurns bool) (wireThread, error) {
	params := map[string]any{"threadId": id, "includeTurns": includeTurns}
	raw, err := a.send(ctx, "thread/read", params)
	if err != nil {
		return wireThread{}, err
	}
	var response struct {
		Thread wireThread `json:"thread"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return wireThread{}, err
	}
	if response.Thread.ID == "" {
		return wireThread{}, errors.New("thread/read returned no thread")
	}
	return response.Thread, nil
}

func (a *nativeThreadAPI) Rename(ctx context.Context, id, name string) error {
	if a.supports != nil && !a.supports(CapabilityThreadRename) {
		return errors.New("thread rename is unavailable")
	}
	name = strings.TrimSpace(name)
	if id == "" || name == "" || len(name) > 256 {
		return errors.New("thread id and bounded name are required")
	}
	_, err := a.send(ctx, "thread/name/set", map[string]any{"threadId": id, "name": name})
	return err
}

func (a *nativeThreadAPI) Archive(ctx context.Context, id string, archived bool) error {
	required := CapabilityThreadArchive
	if !archived {
		required = CapabilityThreadUnarchive
	}
	if a.supports != nil && !a.supports(required) {
		return errors.New("thread archive operation is unavailable")
	}
	method := "thread/archive"
	if !archived {
		method = "thread/unarchive"
	}
	_, err := a.send(ctx, method, map[string]any{"threadId": id})
	return err
}

func (a *nativeThreadAPI) Unsubscribe(ctx context.Context, id string) error {
	if a.supports != nil && !a.supports(CapabilityThreadUnsubscribe) {
		return errors.New("thread unsubscribe is unavailable")
	}
	_, err := a.send(ctx, "thread/unsubscribe", map[string]any{"threadId": id})
	return err
}

func (a *nativeThreadAPI) DeletePreview(ctx context.Context, id string) (provider.ThreadDeletePreview, error) {
	preview := provider.ThreadDeletePreview{}
	cursor := ""
	for pageNo := 0; pageNo < maxThreadFallbackPages && len(preview.DescendantIDs) < maxThreadFallbackRows; pageNo++ {
		page, err := a.List(ctx, provider.ThreadListOptions{AncestorThreadID: id, Cursor: cursor, Limit: maxNativeThreadPage})
		if err != nil {
			return provider.ThreadDeletePreview{}, err
		}
		for _, thread := range page.Threads {
			preview.DescendantIDs = append(preview.DescendantIDs, thread.ID)
			preview.HasLoadedDescendants = preview.HasLoadedDescendants || thread.Loaded
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}
	sort.Strings(preview.DescendantIDs)
	return preview, nil
}

func (a *nativeThreadAPI) Delete(ctx context.Context, id string) (provider.ThreadDeleteResult, error) {
	if a.supports != nil && !a.supports(CapabilityThreadDelete) {
		return provider.ThreadDeleteResult{}, errors.New("permanent thread deletion is unavailable")
	}
	_, err := a.send(ctx, "thread/delete", map[string]any{"threadId": id})
	if err == nil {
		return provider.ThreadDeleteResult{Deleted: true}, nil
	}
	// Unknown write outcomes reconcile with a read. Missing means the delete
	// committed; a surviving thread means the original error remains useful.
	if _, readErr := a.Read(ctx, id, false); readErr != nil {
		return provider.ThreadDeleteResult{Deleted: true, Reconciled: true}, nil
	}
	return provider.ThreadDeleteResult{Reconciled: true}, err
}

func decodeSection(raw json.RawMessage) (provider.ThreadSection, error) {
	var section wireThreadSection
	if err := json.Unmarshal(raw, &section); err != nil {
		return provider.ThreadSection{}, err
	}
	if section.ID == "" || strings.TrimSpace(section.Name) == "" {
		return provider.ThreadSection{}, errors.New("invalid thread section")
	}
	out := provider.ThreadSection{ID: section.ID, Name: boundedPermissionText(section.Name, 256)}
	if section.Appearance != nil {
		out.Icon = boundedPermissionText(section.Appearance.Icon, 64)
		out.Color = boundedPermissionText(section.Appearance.Color, 64)
	}
	return out, nil
}

func (a *nativeThreadAPI) ListSections(ctx context.Context, cursor string, limit int) (provider.ThreadSectionPage, error) {
	if a.supports != nil && !a.supports(CapabilityThreadSectionList) {
		return provider.ThreadSectionPage{}, errors.New("thread sections are unavailable")
	}
	limit = boundedThreadLimit(limit)
	params := map[string]any{"limit": uint32(limit)}
	if cursor != "" {
		params["cursor"] = cursor
	}
	raw, err := a.send(ctx, "threadSection/list", params)
	if err != nil {
		return provider.ThreadSectionPage{}, err
	}
	var response struct {
		Data       []json.RawMessage `json:"data"`
		NextCursor *string           `json:"nextCursor"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return provider.ThreadSectionPage{}, err
	}
	if len(response.Data) > maxThreadSections {
		return provider.ThreadSectionPage{}, errors.New("section page exceeds bound")
	}
	page := provider.ThreadSectionPage{}
	if response.NextCursor != nil {
		page.NextCursor = boundedPermissionText(*response.NextCursor, 1024)
	}
	for _, rawSection := range response.Data {
		section, err := decodeSection(rawSection)
		if err == nil {
			page.Sections = append(page.Sections, section)
		}
	}
	return page, nil
}

func sectionAppearance(mutation provider.ThreadSectionMutation) map[string]string {
	return map[string]string{"icon": mutation.Icon, "color": mutation.Color}
}

func (a *nativeThreadAPI) CreateSection(ctx context.Context, mutation provider.ThreadSectionMutation) (provider.ThreadSection, error) {
	if a.supports != nil && !a.supports(CapabilityThreadSectionCreate) {
		return provider.ThreadSection{}, errors.New("thread section creation is unavailable")
	}
	name := strings.TrimSpace(mutation.Name)
	if name == "" || len(name) > 256 {
		return provider.ThreadSection{}, errors.New("bounded section name required")
	}
	params := map[string]any{"name": name}
	if mutation.AppearanceSet {
		params["appearance"] = sectionAppearance(mutation)
	}
	raw, err := a.send(ctx, "threadSection/create", params)
	if err != nil {
		return provider.ThreadSection{}, err
	}
	var response struct {
		Section json.RawMessage `json:"section"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return provider.ThreadSection{}, err
	}
	return decodeSection(response.Section)
}

func (a *nativeThreadAPI) UpdateSection(ctx context.Context, id string, mutation provider.ThreadSectionMutation) (provider.ThreadSection, error) {
	if a.supports != nil && !a.supports(CapabilityThreadSectionUpdate) {
		return provider.ThreadSection{}, errors.New("thread section update is unavailable")
	}
	name := strings.TrimSpace(mutation.Name)
	if id == "" || name == "" || len(name) > 256 {
		return provider.ThreadSection{}, errors.New("section id and bounded name required")
	}
	params := map[string]any{"sectionId": id, "name": name}
	if mutation.AppearanceSet {
		params["appearance"] = sectionAppearance(mutation)
	}
	raw, err := a.send(ctx, "threadSection/update", params)
	if err != nil {
		return provider.ThreadSection{}, err
	}
	var response struct {
		Section json.RawMessage `json:"section"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return provider.ThreadSection{}, err
	}
	return decodeSection(response.Section)
}

func (a *nativeThreadAPI) DeleteSection(ctx context.Context, id string) error {
	if a.supports != nil && !a.supports(CapabilityThreadSectionDelete) {
		return errors.New("thread section deletion is unavailable")
	}
	_, err := a.send(ctx, "threadSection/delete", map[string]any{"sectionId": id})
	return err
}

func (a *nativeThreadAPI) MoveThread(ctx context.Context, threadID, sectionID, beforeThreadID string) error {
	if a.supports != nil && !a.supports(CapabilityThreadSectionMove) {
		return errors.New("thread section move is unavailable")
	}
	params := map[string]any{"threadId": threadID}
	if sectionID == "" {
		params["sectionId"] = nil
	} else {
		params["sectionId"] = sectionID
	}
	if beforeThreadID != "" {
		params["beforeThreadId"] = beforeThreadID
	}
	_, err := a.send(ctx, "thread/section/move", params)
	return err
}

func (a *nativeThreadAPI) AssignProject(ctx context.Context, threadID string, assignment provider.ProjectAssignment) error {
	if a.supports != nil && !a.supports(CapabilityThreadMetadata) {
		return errors.New("thread project assignment is unavailable")
	}
	params := map[string]any{"threadId": threadID}
	if assignment.Set {
		params["projectId"] = assignment.ProjectID
	}
	_, err := a.send(ctx, "thread/metadata/update", params)
	if err == nil || !assignment.Set || a.supports == nil || !a.supports(CapabilityThreadRead) {
		return err
	}
	thread, readErr := a.Read(ctx, threadID, false)
	if readErr == nil {
		effective := ""
		if thread.ProjectID != nil {
			effective = *thread.ProjectID
		}
		if effective == assignment.ProjectID {
			return nil
		}
	}
	return err
}

func (a *nativeThreadAPI) Fork(ctx context.Context, threadID string) (provider.AgentSessionMeta, error) {
	if a.supports != nil && !a.supports(CapabilityThreadFork) {
		return provider.AgentSessionMeta{}, errors.New("thread fork is unavailable")
	}
	raw, err := a.send(ctx, "thread/fork", map[string]any{"threadId": threadID})
	if err != nil {
		return provider.AgentSessionMeta{}, err
	}
	var response struct {
		Thread wireThread `json:"thread"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return provider.AgentSessionMeta{}, err
	}
	if response.Thread.ID == "" {
		return provider.AgentSessionMeta{}, errors.New("thread/fork returned no thread")
	}
	return projectWireThread(response.Thread, false, true), nil
}

func inheritForkProject(parent, child provider.AgentSessionMeta) provider.AgentSessionMeta {
	if child.ProjectID == "" && child.ForkedFromID == parent.ID {
		child.ProjectID = parent.ProjectID
	}
	return child
}

func (p *Provider) nativeThreads() (*nativeThreadAPI, error) {
	if _, err := p.ensureEngine(context.Background()); err != nil {
		return nil, err
	}
	fr := p.framer()
	if fr == nil {
		return nil, errors.New("Codex engine is not running")
	}
	return newNativeThreadAPI(fr.sendReadOnlyOrWriteRequest, p.supportsCapability), nil
}

// sendReadOnlyOrWriteRequest is intentionally a plain adapter: individual API
// methods decide whether a method is read-only and conn correlation remains
// the one wire implementation.
func (c *conn) sendReadOnlyOrWriteRequest(ctx context.Context, method string, params any) (json.RawMessage, error) {
	readOnly := method == "thread/list" || method == "thread/read" || method == "thread/loaded/list" || method == "thread/search" || method == "thread/turns/list" || method == "thread/items/list" || method == "threadSection/list" || method == "project/list" || method == "project/read"
	return sendWithOverloadRetry(ctx, method, params, readOnly, c.sendRequest, sleepContext)
}

// ListNativeThreads implements provider.NativeThreadBrowser.
func (p *Provider) ListNativeThreads(ctx context.Context, opts provider.ThreadListOptions) (provider.NativeThreadPage, error) {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return provider.NativeThreadPage{}, err
	}
	return api.List(ctx, opts)
}

func (p *Provider) nativeThreadsFor(ctx context.Context) (*nativeThreadAPI, error) {
	if _, err := p.ensureEngine(ctx); err != nil {
		return nil, err
	}
	fr := p.framer()
	if fr == nil {
		return nil, errors.New("Codex engine is not running")
	}
	return newNativeThreadAPI(fr.sendReadOnlyOrWriteRequest, p.supportsCapability), nil
}

// SearchNativeThreads uses native search when negotiated and a bounded stable
// fallback otherwise.
func (p *Provider) SearchNativeThreads(ctx context.Context, opts provider.ThreadSearchOptions) (provider.NativeThreadPage, error) {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return provider.NativeThreadPage{}, err
	}
	return api.Search(ctx, opts)
}

// ListNativeThreadTurns exposes the independently negotiated native turn
// cursor without making the richer thread browser depend on native search.
func (p *Provider) ListNativeThreadTurns(ctx context.Context, opts provider.NativeThreadHistoryOptions) (provider.NativeThreadHistoryPage, error) {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return provider.NativeThreadHistoryPage{}, err
	}
	return api.ListTurns(ctx, opts)
}

// ListNativeThreadItems exposes the independently negotiated native item
// cursor without making it depend on turn pagination.
func (p *Provider) ListNativeThreadItems(ctx context.Context, opts provider.NativeThreadHistoryOptions) (provider.NativeThreadHistoryPage, error) {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return provider.NativeThreadHistoryPage{}, err
	}
	return api.ListItems(ctx, opts)
}

// ReadNativeThread returns metadata only. Explicit replay is performed when a
// managed session resumes the thread.
func (p *Provider) ReadNativeThread(ctx context.Context, id string) (provider.AgentSessionMeta, error) {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return provider.AgentSessionMeta{}, err
	}
	thread, err := api.Read(ctx, id, false)
	if err != nil {
		return provider.AgentSessionMeta{}, err
	}
	loaded, _ := api.loadedIDs(ctx)
	return projectWireThread(thread, false, loaded[id]), nil
}

// SupportsCapability exposes only the boolean latch; denial evidence stays in
// the sanitized runtime snapshot.
func (p *Provider) SupportsCapability(id CapabilityID) bool { return p.supportsCapability(id) }

// ListThreadSections returns one bounded native section page.
func (p *Provider) ListThreadSections(ctx context.Context, cursor string, limit int) (provider.ThreadSectionPage, error) {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return provider.ThreadSectionPage{}, err
	}
	return api.ListSections(ctx, cursor, limit)
}

// RenameNativeThread updates a native thread's display name.
func (p *Provider) RenameNativeThread(ctx context.Context, id, name string) error {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return err
	}
	return api.Rename(ctx, id, name)
}

// ForkNativeThread creates a native child thread.
func (p *Provider) ForkNativeThread(ctx context.Context, id string) (provider.AgentSessionMeta, error) {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return provider.AgentSessionMeta{}, err
	}
	return api.Fork(ctx, id)
}

// ArchiveNativeThread archives or restores a native thread.
func (p *Provider) ArchiveNativeThread(ctx context.Context, id string, archived bool) error {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return err
	}
	return api.Archive(ctx, id, archived)
}

// PreviewDeleteNativeThread reports descendant impact without mutating state.
func (p *Provider) PreviewDeleteNativeThread(ctx context.Context, id string) (provider.ThreadDeletePreview, error) {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return provider.ThreadDeletePreview{}, err
	}
	return api.DeletePreview(ctx, id)
}

// DeleteNativeThread permanently deletes a native thread and reconciles its
// descendant result.
func (p *Provider) DeleteNativeThread(ctx context.Context, id string) (provider.ThreadDeleteResult, error) {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return provider.ThreadDeleteResult{}, err
	}
	preview, err := api.DeletePreview(ctx, id)
	if err != nil {
		return provider.ThreadDeleteResult{}, err
	}
	result, err := api.Delete(ctx, id)
	if err != nil {
		return result, err
	}
	result.DescendantIDs = append([]string(nil), preview.DescendantIDs...)
	for _, descendantID := range preview.DescendantIDs {
		if _, readErr := api.Read(ctx, descendantID, false); readErr == nil {
			result.FailedDescendantIDs = append(result.FailedDescendantIDs, descendantID)
		}
	}
	result.Partial = len(result.FailedDescendantIDs) > 0
	return result, nil
}

// CreateThreadSection creates a native thread section.
func (p *Provider) CreateThreadSection(ctx context.Context, mutation provider.ThreadSectionMutation) (provider.ThreadSection, error) {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return provider.ThreadSection{}, err
	}
	return api.CreateSection(ctx, mutation)
}

// UpdateThreadSection replaces a native section's visible fields.
func (p *Provider) UpdateThreadSection(ctx context.Context, id string, mutation provider.ThreadSectionMutation) (provider.ThreadSection, error) {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return provider.ThreadSection{}, err
	}
	return api.UpdateSection(ctx, id, mutation)
}

// DeleteThreadSection deletes a section while preserving its member threads.
func (p *Provider) DeleteThreadSection(ctx context.Context, id string) error {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return err
	}
	return api.DeleteSection(ctx, id)
}

// MoveNativeThread reorders or moves a native thread between sections.
func (p *Provider) MoveNativeThread(ctx context.Context, threadID, sectionID, beforeThreadID string) error {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return err
	}
	return api.MoveThread(ctx, threadID, sectionID, beforeThreadID)
}

// AssignNativeThreadProject applies the exact omit, clear, or assign state.
func (p *Provider) AssignNativeThreadProject(ctx context.Context, threadID string, assignment provider.ProjectAssignment) error {
	api, err := p.nativeThreadsFor(ctx)
	if err != nil {
		return err
	}
	return api.AssignProject(ctx, threadID, assignment)
}

// ListAgentSessions preserves the provider-neutral metadata discovery path.
func (p *Provider) ListAgentSessions(ctx context.Context) ([]provider.AgentSessionMeta, error) {
	page, err := p.ListNativeThreads(ctx, provider.ThreadListOptions{Limit: maxNativeThreadPage})
	return page.Threads, err
}

func (s *session) emitThreadReplay(raw json.RawMessage) error {
	var response struct {
		Thread wireThread `json:"thread"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return err
	}
	for _, turn := range response.Thread.Turns {
		for _, rawItem := range turn.Items {
			var item struct {
				Type    string `json:"type"`
				ID      string `json:"id"`
				Text    string `json:"text"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			}
			if json.Unmarshal(rawItem, &item) != nil {
				continue
			}
			now := time.Now().UTC()
			switch item.Type {
			case "userMessage":
				var text strings.Builder
				for _, content := range item.Content {
					if content.Type == "text" && content.Text != "" {
						if text.Len() > 0 {
							text.WriteByte('\n')
						}
						text.WriteString(content.Text)
					}
				}
				if text.Len() > 0 {
					s.emit(event.Event{Type: event.TypeUserMessage, SessionID: s.localID, AgentSessionID: s.agentID, Timestamp: now, Text: text.String(), ToolID: item.ID, Replay: true})
				}
			case "agentMessage":
				if strings.TrimSpace(item.Text) != "" {
					s.emit(event.Event{Type: event.TypeAssistantChunk, SessionID: s.localID, AgentSessionID: s.agentID, Timestamp: now, Text: item.Text, ToolID: item.ID, Replay: true})
				}
			}
		}
	}
	return nil
}

func (s *session) replayThreadHistory(ctx context.Context, fr *conn) error {
	raw, err := fr.sendReadOnlyOrWriteRequest(ctx, "thread/read", map[string]any{"threadId": s.AgentSessionID(), "includeTurns": true})
	if err != nil {
		return fmt.Errorf("thread/read replay: %w", err)
	}
	return s.emitThreadReplay(raw)
}

var _ provider.AgentSessionLister = (*Provider)(nil)
var _ provider.NativeThreadBrowser = (*Provider)(nil)
