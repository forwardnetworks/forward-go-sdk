package forward

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Bulk reads of NQE library query SOURCE. Forward has no bulk source read on the head listing (ListHeadQueries returns path, last commit
// and query id only; ?with=sourceCode exists only beside a single ?path=), but it has two export routes that take a set of paths and answer
// a ZIP of the sources, built for import into another org: NqeLibController.exportWorkspacePathsOrg and exportHeadPathsFwd. Their ZIP holds one entry,
// "queries-export.proto" (NqeLibExport.QUERY_EXPORT_FILE_NAME), a protobuf NqeLibExportPB { repeated ExportedQueryPB queries = 1 } with
// ExportedQueryPB { QueryPathPB path = 1; NqeQuerySourcePB source_code = 2 }, both wrappers a message with one string field 1
// (core MainNqeLibProtos.proto / NqeLibProtos.proto at origin/master). Preview and source-only: not run against a live Forward.

const nqeExportEntry = "queries-export.proto"

// maxNQEExportBytes bounds the ZIP and the decoded protobuf, so a runaway answer cannot fill memory.
const maxNQEExportBytes = 256 << 20

// ExportOrgSources returns the source of every query in the organization library under paths, as a map from query path to source, in one
// request (POST /api/users/current/nqe/changes?action=export; exportWorkspacePathsOrg, VIEW_NQE_LIBRARY). A path ending in "/" is a
// directory (everything under it, so {"/"} is the whole library) and any other is one query. Two behaviours to know: it is the CALLING
// USER'S view of the library, the committed head with that user's uncommitted drafts applied (getUserEffectiveQueries), not a fixed
// commit; and it also includes the non-Forward queries the requested ones import. An empty paths list returns an empty map. A path
// that matches nothing is simply absent.
func (s *NQERepositoryService) ExportOrgSources(ctx context.Context, paths []string) (map[string]string, *Response, error) {
	return s.exportSources(ctx, "/api/users/current/nqe/changes?action=export", "NQERepository.ExportOrgSources", paths)
}

// ExportForwardSources is ExportOrgSources for the Forward Library, Forward's own shipped queries (POST
// /api/nqe/repos/fwd/commits/head/queries?action=export; exportHeadPathsFwd). The queries the org's license tier does not permit are
// left out by Forward.
func (s *NQERepositoryService) ExportForwardSources(ctx context.Context, paths []string) (map[string]string, *Response, error) {
	return s.exportSources(ctx, "/api/nqe/repos/fwd/commits/head/queries?action=export", "NQERepository.ExportForwardSources", paths)
}

func (s *NQERepositoryService) exportSources(ctx context.Context, path, operation string, paths []string) (map[string]string, *Response, error) {
	if len(paths) == 0 {
		return map[string]string{}, nil, nil
	}
	clean := make([]string, 0, len(paths))
	for _, p := range paths {
		if p = strings.TrimSpace(p); p == "" || !strings.HasPrefix(p, "/") {
			return nil, nil, fmt.Errorf("forward: NQE library path %q must start with / (a directory ends with / too)", p)
		}
		clean = append(clean, p)
	}
	req, err := s.client.newJSONRequest(ctx, http.MethodPost, path, map[string]any{"paths": clean})
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/zip")
	var body bytes.Buffer
	resp, err := s.client.Do(markOperation(req, operation), &nqeExportWriter{w: &body, remaining: maxNQEExportBytes})
	if err != nil {
		return nil, resp, err
	}
	sources, err := decodeNQEExportZip(body.Bytes())
	return sources, resp, err
}

// nqeExportWriter refuses to grow past its budget, so a runaway response body cannot exhaust memory.
type nqeExportWriter struct {
	w         io.Writer
	remaining int64
}

func (l *nqeExportWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.remaining {
		return 0, fmt.Errorf("forward: the NQE export is larger than %d bytes", int64(maxNQEExportBytes))
	}
	n, err := l.w.Write(p)
	l.remaining -= int64(n)
	return n, err
}

func decodeNQEExportZip(raw []byte) (map[string]string, error) {
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("forward: the NQE export is not a ZIP: %w", err)
	}
	for _, entry := range archive.File {
		if entry.Name != nqeExportEntry {
			continue
		}
		if entry.UncompressedSize64 > maxNQEExportBytes {
			return nil, errors.New("forward: the NQE export is larger than the SDK reads")
		}
		file, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("forward: open the NQE export: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(file, maxNQEExportBytes+1))
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("forward: read the NQE export: %w", err)
		}
		if len(data) > maxNQEExportBytes {
			return nil, errors.New("forward: the NQE export is larger than the SDK reads")
		}
		return decodeNQELibExport(data)
	}
	return nil, fmt.Errorf("forward: the NQE export has no %s entry", nqeExportEntry)
}

// decodeNQELibExport reads NqeLibExportPB with the protobuf wire format by hand (the SDK takes no protobuf dependency): the schema is two
// nested one-string messages, so a field reader is all it needs. Unknown fields are skipped.
func decodeNQELibExport(data []byte) (map[string]string, error) {
	out := map[string]string{}
	err := forEachPBField(data, func(field int, value []byte) error {
		if field != 1 {
			return nil
		}
		var path, source string
		if err := forEachPBField(value, func(f int, v []byte) error {
			switch f {
			case 1:
				return forEachPBField(v, func(g int, s []byte) error {
					if g == 1 {
						path = string(s)
					}
					return nil
				})
			case 2:
				return forEachPBField(v, func(g int, s []byte) error {
					if g == 1 {
						source = string(s)
					}
					return nil
				})
			}
			return nil
		}); err != nil {
			return err
		}
		if path == "" {
			return errors.New("an exported query has no path")
		}
		out[path] = source
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("forward: decode the NQE export: %w", err)
	}
	return out, nil
}

// forEachPBField calls fn with each length-delimited field (wire type 2) of one protobuf message; other wire types are skipped.
func forEachPBField(data []byte, fn func(field int, value []byte) error) error {
	for len(data) > 0 {
		tag, n := pbVarint(data)
		if n <= 0 {
			return errors.New("bad field tag")
		}
		data = data[n:]
		field, wire := int(tag>>3), int(tag&7)
		switch wire {
		case 0:
			_, n = pbVarint(data)
			if n <= 0 {
				return errors.New("bad varint")
			}
			data = data[n:]
		case 1:
			if len(data) < 8 {
				return errors.New("short fixed64")
			}
			data = data[8:]
		case 5:
			if len(data) < 4 {
				return errors.New("short fixed32")
			}
			data = data[4:]
		case 2:
			length, n := pbVarint(data)
			if n <= 0 || length > uint64(len(data)-n) {
				return errors.New("bad length")
			}
			value := data[n : n+int(length)]
			data = data[n+int(length):]
			if err := fn(field, value); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported wire type %d", wire)
		}
	}
	return nil
}

func pbVarint(data []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(data) && i < 10; i++ {
		v |= uint64(data[i]&0x7f) << (7 * uint(i))
		if data[i] < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}
