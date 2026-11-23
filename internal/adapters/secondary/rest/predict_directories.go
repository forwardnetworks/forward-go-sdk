package rest

import (
	"context"
	"errors"
	"github.com/forwardnetworks/forward-go-sdk/ports"
	"net/http"
	"net/url"
	"strings"
)

// Change-set directories (ChangeSetController, MANAGE_CHANGE_SETS and the organization property PREDICT_MODELING). Preview: not in
// the published OpenAPI. A directory path is "/" for the root or "/a/b", never ending in "/"; it travels as one escaped path segment.

func (s *Predict) dirsPath(networkID string, rest ...string) (string, error) {
	return s.client.networkSubPath(networkID, append([]string{"change-set-directories"}, rest...)...)
}

func (s *Predict) dirAction(ctx context.Context, networkID, dirPath, action, operation string, query url.Values, body any) (*ports.Response, error) {
	dirPath = strings.TrimSpace(dirPath)
	if !validDirPath(dirPath) {
		return nil, errors.New(`forward: a change-set directory path is "/" or starts with "/", does not end with "/" and has no empty level`)
	}
	path, err := s.dirsPath(networkID, url.PathEscape(dirPath))
	if err != nil {
		return nil, err
	}
	if query == nil {
		query = url.Values{}
	}
	if action != "" {
		query.Set("action", action)
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return s.doJSON(ctx, http.MethodPost, path, body, nil, operation)
}

// ListChangeSetDirectories returns the directory tree as path to change-set ids (GET .../change-set-directories). A network with
// no directories answers an empty map.
func (s *Predict) ListChangeSetDirectories(ctx context.Context, networkID string) (ports.ChangeSetDirectories, *ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, nil, err
	}
	path, err := s.dirsPath(networkID)
	if err != nil {
		return nil, nil, err
	}
	out := ports.ChangeSetDirectories{}
	resp, err := s.doJSON(ctx, http.MethodGet, path, nil, &out, "Predict.ListChangeSetDirectories")
	return out, resp, err
}

// AddChangeSetDirectory creates the directory name under parentPath (POST .../change-set-directories/{parentPath}?action=addDir&name=).
func (s *Predict) AddChangeSetDirectory(ctx context.Context, networkID, parentPath, name string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if name = strings.TrimSpace(name); name == "" || strings.Contains(name, "/") {
		return nil, errors.New("forward: a directory name is required and cannot contain '/'")
	}
	return s.dirAction(ctx, networkID, parentPath, "addDir", "Predict.AddChangeSetDirectory", url.Values{"name": []string{name}}, nil)
}

// RenameChangeSetDirectory renames the directory at dirPath (?action=rename&name=).
func (s *Predict) RenameChangeSetDirectory(ctx context.Context, networkID, dirPath, newName string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if newName = strings.TrimSpace(newName); newName == "" || strings.Contains(newName, "/") {
		return nil, errors.New("forward: a directory name is required and cannot contain '/'")
	}
	if strings.TrimSpace(dirPath) == "/" {
		return nil, errors.New("forward: the root directory cannot be renamed")
	}
	return s.dirAction(ctx, networkID, dirPath, "rename", "Predict.RenameChangeSetDirectory", url.Values{"name": []string{newName}}, nil)
}

// MoveChangeSetDirectory moves the directory at dirPath, with what is in it, under destPath (?action=move&to=).
func (s *Predict) MoveChangeSetDirectory(ctx context.Context, networkID, dirPath, destPath string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(dirPath) == "/" {
		return nil, errors.New("forward: the root directory cannot be moved")
	}
	destPath = strings.TrimSpace(destPath)
	if !validDirPath(destPath) {
		return nil, errors.New(`forward: the destination is "/" or starts with "/" and does not end with "/"`)
	}
	return s.dirAction(ctx, networkID, dirPath, "move", "Predict.MoveChangeSetDirectory", url.Values{"to": []string{destPath}}, nil)
}

// MoveChangeSetToDirectory puts one change set in the directory at dirPath (?action=moveChangeSetTo&changeSetId=).
func (s *Predict) MoveChangeSetToDirectory(ctx context.Context, networkID, dirPath, changeSetID string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	if changeSetID = strings.TrimSpace(changeSetID); changeSetID == "" {
		return nil, errors.New("forward: change-set ID is required")
	}
	return s.dirAction(ctx, networkID, dirPath, "moveChangeSetTo", "Predict.MoveChangeSetToDirectory", url.Values{"changeSetId": []string{changeSetID}}, nil)
}

// MoveChangeSetsToDirectory puts several change sets in the directory at dirPath (?action=moveChangeSetsTo, body {changeSetIds}).
func (s *Predict) MoveChangeSetsToDirectory(ctx context.Context, networkID, dirPath string, changeSetIDs []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	ids, err := cleanIDs(changeSetIDs)
	if err != nil {
		return nil, err
	}
	return s.dirAction(ctx, networkID, dirPath, "moveChangeSetsTo", "Predict.MoveChangeSetsToDirectory", nil, changeSetIDsBody{ChangeSetIDs: ids})
}

// DeleteChangeSetDirectory deletes the directory at dirPath (DELETE .../change-set-directories/{dirPath}). By default the change sets
// in it are kept; Forward moves them up (the shallow delete). With alsoDeleteChangeSets it deletes them too, in the directory and in
// every directory below it (?retainChangeSets=false), which cannot be undone and also deletes those change sets' predicted snapshots.
func (s *Predict) DeleteChangeSetDirectory(ctx context.Context, networkID, dirPath string, alsoDeleteChangeSets bool) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	dirPath = strings.TrimSpace(dirPath)
	if dirPath == "/" || !validDirPath(dirPath) {
		return nil, errors.New(`forward: a directory path starting with "/" (not the root) is required`)
	}
	path, err := s.dirsPath(networkID, url.PathEscape(dirPath))
	if err != nil {
		return nil, err
	}
	if alsoDeleteChangeSets {
		path += "?retainChangeSets=false"
	}
	return s.doJSON(ctx, http.MethodDelete, path, nil, nil, "Predict.DeleteChangeSetDirectory")
}

// validDirPath: Forward refuses (400) a path that does not start with "/", ends with "/" (except the root) or has an empty level.
func validDirPath(p string) bool {
	return p == "/" || (strings.HasPrefix(p, "/") && !strings.HasSuffix(p, "/") && !strings.Contains(p, "//"))
}

type changeSetIDsBody struct {
	ChangeSetIDs []string `json:"changeSetIds"`
}

// cleanIDs trims, drops blanks and duplicates, and refuses an empty result: Forward answers 400 for no ids.
func cleanIDs(ids []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("forward: at least one change-set ID is required")
	}
	return out, nil
}

// DeleteChangeSets deletes several change sets of the network (POST .../change-sets?action=delete, body {changeSetIds}). Use
// DeleteChangeSet for one. It cannot be undone, and like DeleteChangeSet it also deletes their predicted snapshots.
func (s *Predict) DeleteChangeSets(ctx context.Context, networkID string, changeSetIDs []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	ids, err := cleanIDs(changeSetIDs)
	if err != nil {
		return nil, err
	}
	path, err := s.client.networkSubPath(networkID, "change-sets")
	if err != nil {
		return nil, err
	}
	return s.doJSON(ctx, http.MethodPost, path+"?action=delete", changeSetIDsBody{ChangeSetIDs: ids}, nil, "Predict.DeleteChangeSets")
}

// PatchChangeSetsTags adds or removes tags on several change sets at once (PATCH .../change-sets). ChangeSetTagsRemoveAll takes no
// names; the other two need at least one.
func (s *Predict) PatchChangeSetsTags(ctx context.Context, networkID string, changeSetIDs []string, action ports.ChangeSetTagAction, names []string) (*ports.Response, error) {
	if err := (*service)(s).failClosed(); err != nil {
		return nil, err
	}
	ids, err := cleanIDs(changeSetIDs)
	if err != nil {
		return nil, err
	}
	type tags struct {
		Action ports.ChangeSetTagAction `json:"action"`
		Names  []string                 `json:"names,omitempty"`
	}
	t := tags{Action: action}
	switch action {
	case ports.ChangeSetTagsRemoveAll:
		if len(names) > 0 {
			return nil, errors.New("forward: REMOVE_ALL takes no tag names")
		}
	case ports.ChangeSetTagsAdd, ports.ChangeSetTagsRemove:
		for _, n := range names {
			if n = strings.TrimSpace(n); n != "" {
				t.Names = append(t.Names, n)
			}
		}
		if len(t.Names) == 0 {
			return nil, errors.New("forward: at least one tag name is required")
		}
	default:
		return nil, errors.New("forward: tag action must be ADD, REMOVE or REMOVE_ALL")
	}
	path, err := s.client.networkSubPath(networkID, "change-sets")
	if err != nil {
		return nil, err
	}
	body := struct {
		ChangeSetIDs []string `json:"changeSetIds"`
		Update       struct {
			Tags tags `json:"tags"`
		} `json:"update"`
	}{ChangeSetIDs: ids}
	body.Update.Tags = t
	return s.doJSON(ctx, http.MethodPatch, path, body, nil, "Predict.PatchChangeSetsTags")
}
