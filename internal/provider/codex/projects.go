package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

const (
	upstreamProjectDefaultLimit = 25
	upstreamProjectMaximumLimit = 100
	projectListLimit            = 50
	maxProjectPages             = 20
	maxProjects                 = 1000
)

type projectAPI struct {
	send     nativeRPCSender
	supports nativeCapability
}

func newProjectAPI(send nativeRPCSender, supports nativeCapability) *projectAPI {
	return &projectAPI{send: send, supports: supports}
}

func (a *projectAPI) require(id CapabilityID) error {
	if a.supports == nil || !a.supports(id) {
		return fmt.Errorf("Codex capability %s is unavailable", id)
	}
	return nil
}

type wireProject struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Roots []struct {
		Path string `json:"path"`
	} `json:"roots"`
	Metadata  map[string]string `json:"metadata"`
	Position  int64             `json:"position"`
	CreatedAt int64             `json:"createdAt"`
	UpdatedAt int64             `json:"updatedAt"`
}

func projectWireProject(in wireProject) (provider.Project, error) {
	if in.ID == "" || strings.TrimSpace(in.Name) == "" {
		return provider.Project{}, errors.New("invalid project response")
	}
	out := provider.Project{
		ID: in.ID, Name: boundedPermissionText(in.Name, 256), Metadata: in.Metadata,
		Position: in.Position, CreatedAt: unixTime(in.CreatedAt), UpdatedAt: unixTime(in.UpdatedAt),
	}
	for _, root := range in.Roots {
		if filepath.IsAbs(root.Path) {
			out.Roots = append(out.Roots, filepath.Clean(root.Path))
		}
	}
	return out, nil
}

func boundedProjectLimit(limit int) int {
	if limit <= 0 {
		return projectListLimit
	}
	if limit > upstreamProjectMaximumLimit {
		return upstreamProjectMaximumLimit
	}
	return limit
}

func (a *projectAPI) List(ctx context.Context, cursor string, limit int) (provider.ProjectPage, error) {
	if err := a.require(CapabilityProjectList); err != nil {
		return provider.ProjectPage{}, err
	}
	limit = boundedProjectLimit(limit)
	params := map[string]any{"limit": uint32(limit)}
	if cursor != "" {
		params["cursor"] = cursor
	}
	raw, err := a.send(ctx, "project/list", params)
	if err != nil {
		return provider.ProjectPage{}, err
	}
	var response struct {
		Data       []wireProject `json:"data"`
		NextCursor *string       `json:"nextCursor"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return provider.ProjectPage{}, err
	}
	if len(response.Data) > upstreamProjectMaximumLimit {
		return provider.ProjectPage{}, errors.New("project page exceeds upstream maximum")
	}
	page := provider.ProjectPage{Limit: limit}
	if response.NextCursor != nil {
		page.NextCursor = boundedPermissionText(*response.NextCursor, 1024)
	}
	for _, rawProject := range response.Data {
		project, err := projectWireProject(rawProject)
		if err == nil {
			page.Projects = append(page.Projects, project)
		}
	}
	sort.SliceStable(page.Projects, func(i, j int) bool {
		if page.Projects[i].Position != page.Projects[j].Position {
			return page.Projects[i].Position < page.Projects[j].Position
		}
		return page.Projects[i].ID < page.Projects[j].ID
	})
	return page, nil
}

func decodeProjectEnvelope(raw json.RawMessage) (provider.Project, error) {
	var response struct {
		Project wireProject `json:"project"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return provider.Project{}, err
	}
	return projectWireProject(response.Project)
}

func (a *projectAPI) Read(ctx context.Context, id string) (provider.Project, error) {
	if err := a.require(CapabilityProjectRead); err != nil {
		return provider.Project{}, err
	}
	raw, err := a.send(ctx, "project/read", map[string]any{"projectId": id})
	if err != nil {
		return provider.Project{}, err
	}
	return decodeProjectEnvelope(raw)
}

func canonicalProjectRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if !filepath.IsAbs(root) {
		return "", errors.New("project roots must be absolute")
	}
	logical := filepath.Clean(root)
	canonical, err := filepath.EvalSymlinks(logical)
	if err == nil {
		return filepath.Clean(canonical), nil
	}
	return logical, nil
}

func validateProjectMutation(mutation provider.ProjectMutation) error {
	name := strings.TrimSpace(mutation.Name)
	if name == "" || len(name) > 256 {
		return errors.New("project name must be non-blank and bounded")
	}
	logicalSeen := make(map[string]bool, len(mutation.Roots))
	canonicalSeen := make(map[string]bool, len(mutation.Roots))
	for _, root := range mutation.Roots {
		trimmed := strings.TrimSpace(root)
		if !filepath.IsAbs(trimmed) {
			return errors.New("project roots must be absolute")
		}
		logical := filepath.Clean(trimmed)
		if logicalSeen[logical] {
			return errors.New("duplicate project root")
		}
		logicalSeen[logical] = true
		canonical, err := canonicalProjectRoot(logical)
		if err != nil {
			return err
		}
		if canonicalSeen[canonical] {
			return errors.New("duplicate canonical project root")
		}
		canonicalSeen[canonical] = true
	}
	threadSeen := make(map[string]bool, len(mutation.ThreadIDs))
	for _, id := range mutation.ThreadIDs {
		id = strings.TrimSpace(id)
		if id == "" || threadSeen[id] {
			return errors.New("project import thread ids must be unique and non-empty")
		}
		threadSeen[id] = true
	}
	return nil
}

func projectMutationParams(mutation provider.ProjectMutation, idempotent bool) map[string]any {
	roots := make([]map[string]string, 0, len(mutation.Roots))
	for _, root := range mutation.Roots {
		roots = append(roots, map[string]string{"path": filepath.Clean(strings.TrimSpace(root))})
	}
	params := map[string]any{"name": strings.TrimSpace(mutation.Name), "roots": roots}
	if len(mutation.Metadata) > 0 {
		params["metadata"] = mutation.Metadata
	}
	if len(mutation.ThreadIDs) > 0 {
		params["threads"] = append([]string(nil), mutation.ThreadIDs...)
	}
	if idempotent {
		params["idempotencyKey"] = mutation.IdempotencyKey
	}
	return params
}

func (a *projectAPI) createOrImport(ctx context.Context, method string, capability CapabilityID, mutation provider.ProjectMutation) (provider.Project, error) {
	if err := a.require(capability); err != nil {
		return provider.Project{}, err
	}
	if err := validateProjectMutation(mutation); err != nil {
		return provider.Project{}, err
	}
	if strings.TrimSpace(mutation.IdempotencyKey) == "" {
		return provider.Project{}, errors.New("idempotency key required")
	}
	raw, err := a.send(ctx, method, projectMutationParams(mutation, true))
	if err != nil {
		return provider.Project{}, err
	}
	return decodeProjectEnvelope(raw)
}

func (a *projectAPI) Create(ctx context.Context, mutation provider.ProjectMutation) (provider.Project, error) {
	return a.createOrImport(ctx, "project/create", CapabilityProjectCreate, mutation)
}

func (a *projectAPI) Import(ctx context.Context, mutation provider.ProjectMutation) (provider.Project, error) {
	return a.createOrImport(ctx, "project/import", CapabilityProjectImport, mutation)
}

func (a *projectAPI) Update(ctx context.Context, id string, mutation provider.ProjectMutation) (provider.Project, error) {
	if err := a.require(CapabilityProjectUpdate); err != nil {
		return provider.Project{}, err
	}
	if err := validateProjectMutation(mutation); err != nil {
		return provider.Project{}, err
	}
	params := projectMutationParams(mutation, false)
	params["projectId"] = id
	raw, err := a.send(ctx, "project/update", params)
	if err != nil {
		// Ambiguous updates reconcile by read; callers see the effective state.
		if reconciled, readErr := a.Read(ctx, id); readErr == nil {
			return reconciled, nil
		}
		return provider.Project{}, err
	}
	return decodeProjectEnvelope(raw)
}

func (a *projectAPI) Move(ctx context.Context, id, beforeID string) error {
	if err := a.require(CapabilityProjectMove); err != nil {
		return err
	}
	params := map[string]any{"projectId": id}
	if beforeID != "" {
		params["beforeProjectId"] = beforeID
	}
	_, err := a.send(ctx, "project/move", params)
	if err != nil {
		if a.supports == nil || !a.supports(CapabilityProjectRead) {
			return err
		}
		_, readErr := a.Read(ctx, id)
		if readErr == nil {
			return nil
		}
	}
	return err
}

func (a *projectAPI) Delete(ctx context.Context, id string) error {
	if err := a.require(CapabilityProjectDelete); err != nil {
		return err
	}
	_, err := a.send(ctx, "project/delete", map[string]any{"projectId": id})
	if err != nil {
		if a.supports == nil || !a.supports(CapabilityProjectRead) {
			return err
		}
		if _, readErr := a.Read(ctx, id); readErr != nil {
			return nil
		}
	}
	return err
}

func (p *Provider) projectAPIFor(ctx context.Context) (*projectAPI, error) {
	if _, err := p.ensureEngine(ctx); err != nil {
		return nil, err
	}
	fr := p.framer()
	if fr == nil {
		return nil, errors.New("Codex engine is not running")
	}
	return newProjectAPI(fr.sendReadOnlyOrWriteRequest, p.supportsCapability), nil
}

// ListProjects follows at most 20 pages or 1000 projects and always sends the
// plan-mandated explicit limit of 50.
func (p *Provider) ListProjects(ctx context.Context) (provider.ProjectPage, error) {
	api, err := p.projectAPIFor(ctx)
	if err != nil {
		return provider.ProjectPage{}, err
	}
	out := provider.ProjectPage{Limit: projectListLimit}
	cursor := ""
	for pageNo := 0; pageNo < maxProjectPages && len(out.Projects) < maxProjects; pageNo++ {
		page, err := api.List(ctx, cursor, projectListLimit)
		if err != nil {
			return provider.ProjectPage{}, err
		}
		out.Projects = append(out.Projects, page.Projects...)
		if page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}
	if len(out.Projects) >= maxProjects || cursor != "" {
		out.Truncated = true
	}
	if len(out.Projects) > maxProjects {
		out.Projects = out.Projects[:maxProjects]
	}
	return out, nil
}

// ReadProject returns one native project projection.
func (p *Provider) ReadProject(ctx context.Context, id string) (provider.Project, error) {
	api, err := p.projectAPIFor(ctx)
	if err != nil {
		return provider.Project{}, err
	}
	return api.Read(ctx, id)
}

// CreateProject creates an idempotent native project.
func (p *Provider) CreateProject(ctx context.Context, mutation provider.ProjectMutation) (provider.Project, error) {
	api, err := p.projectAPIFor(ctx)
	if err != nil {
		return provider.Project{}, err
	}
	return api.Create(ctx, mutation)
}

// ImportProject atomically imports a project and its thread assignments.
func (p *Provider) ImportProject(ctx context.Context, mutation provider.ProjectMutation) (provider.Project, error) {
	api, err := p.projectAPIFor(ctx)
	if err != nil {
		return provider.Project{}, err
	}
	return api.Import(ctx, mutation)
}

// UpdateProject replaces a native project's visible fields.
func (p *Provider) UpdateProject(ctx context.Context, id string, mutation provider.ProjectMutation) (provider.Project, error) {
	api, err := p.projectAPIFor(ctx)
	if err != nil {
		return provider.Project{}, err
	}
	return api.Update(ctx, id, mutation)
}

// MoveProject reorders a native project.
func (p *Provider) MoveProject(ctx context.Context, id, beforeID string) error {
	api, err := p.projectAPIFor(ctx)
	if err != nil {
		return err
	}
	return api.Move(ctx, id, beforeID)
}

// DeleteProject deletes a project while preserving threads and roots.
func (p *Provider) DeleteProject(ctx context.Context, id string) error {
	api, err := p.projectAPIFor(ctx)
	if err != nil {
		return err
	}
	return api.Delete(ctx, id)
}
