package gitops

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// manifestFile is one YAML file from a workload's folder, path relative to the repo root.
type manifestFile struct {
	Path string
	Data []byte
}

// fieldEdit records one value the patch changed.
type fieldEdit struct {
	Field string // cpu, memory, replicas
	Old   string // "" when the key did not exist
	New   string
}

// patchResult is the outcome of applying a change set to a workload's manifests.
type patchResult struct {
	Path    string // file that holds the Deployment
	Data    []byte // patched content; equals the original when Edits is empty
	Edits   []fieldEdit
	Skipped []string // human-readable reasons for fields that were not applied
}

// patchWorkload applies cs to the Deployment called name. It edits the original bytes line by
// line instead of re-encoding, so comments, quoting and unrelated documents are untouched.
func patchWorkload(files []manifestFile, name string, cs ChangeSet) (patchResult, error) {
	fileIdx, docIdx, hpaManaged, err := locateDeployment(files, name)
	if err != nil {
		return patchResult{}, err
	}
	res := patchResult{Path: files[fileIdx].Path, Data: files[fileIdx].Data}

	// Each edit re-parses the file so line numbers always match the current bytes.
	containerEdit := func(field, value string) error {
		dep, err := deploymentDoc(res.Data, docIdx)
		if err != nil {
			return err
		}
		containers := lookup(dep, "spec", "template", "spec", "containers")
		if containers == nil || containers.Kind != yaml.SequenceNode || len(containers.Content) == 0 {
			return fmt.Errorf("deployment %s has no containers list", name)
		}
		if n := len(containers.Content); n != 1 {
			res.Skipped = append(res.Skipped, fmt.Sprintf(
				"%s: pod has %d containers and the recommendation is a pod total; edit by hand", field, n))
			return nil
		}
		return res.set(containers.Content[0], []string{"resources", "requests", field}, field, value, true)
	}

	if cs.CPU != "" {
		if err := containerEdit("cpu", cs.CPU); err != nil {
			return patchResult{}, err
		}
	}
	if cs.Memory != "" {
		if err := containerEdit("memory", cs.Memory); err != nil {
			return patchResult{}, err
		}
	}
	if cs.Replicas != nil {
		if hpaManaged {
			res.Skipped = append(res.Skipped, "replicas: a HorizontalPodAutoscaler manages this Deployment")
		} else {
			dep, err := deploymentDoc(res.Data, docIdx)
			if err != nil {
				return patchResult{}, err
			}
			spec := lookup(dep, "spec")
			if spec == nil || spec.Kind != yaml.MappingNode {
				return patchResult{}, fmt.Errorf("deployment %s has no spec mapping", name)
			}
			value := strconv.FormatInt(int64(*cs.Replicas), 10)
			if err := res.set(spec, []string{"replicas"}, "replicas", value, false); err != nil {
				return patchResult{}, err
			}
		}
	}

	if len(res.Edits) > 0 {
		if err := verifyPatch(res.Data, docIdx, res.Edits); err != nil {
			return patchResult{}, fmt.Errorf("patched %s does not read back: %w", res.Path, err)
		}
	}
	return res, nil
}

// set writes value at keys below parent, replacing an existing scalar or inserting the missing keys.
func (r *patchResult) set(parent *yaml.Node, keys []string, field, value string, quoteNew bool) error {
	node := parent
	for i, key := range keys {
		child := mappingValue(node, key)
		if child == nil {
			data, err := insertKeys(r.Data, node, keys[i:], value, quoteNew)
			if err != nil {
				return fmt.Errorf("%s: %w", field, err)
			}
			r.Data = data
			r.Edits = append(r.Edits, fieldEdit{Field: field, New: value})
			return nil
		}
		if i < len(keys)-1 {
			if child.Kind != yaml.MappingNode || child.Style&yaml.FlowStyle != 0 || len(child.Content) == 0 {
				return fmt.Errorf("%s: %q is not a block mapping; edit by hand", field, key)
			}
			node = child
			continue
		}
		if child.Kind != yaml.ScalarNode {
			return fmt.Errorf("%s: %q is not a scalar", field, key)
		}
		if child.Value == value {
			return nil
		}
		data, err := replaceScalar(r.Data, child, value)
		if err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
		r.Data = data
		r.Edits = append(r.Edits, fieldEdit{Field: field, Old: child.Value, New: value})
	}
	return nil
}

// locateDeployment finds the file and document index of the Deployment, and whether an HPA in
// any of the files targets it.
func locateDeployment(files []manifestFile, name string) (fileIdx, docIdx int, hpaManaged bool, err error) {
	fileIdx, docIdx = -1, -1
	for fi, f := range files {
		docs, err := decodeAll(f.Data)
		if err != nil {
			return -1, -1, false, fmt.Errorf("parse %s: %w", f.Path, err)
		}
		for di, doc := range docs {
			switch scalarAt(doc, "kind") {
			case "Deployment":
				if scalarAt(doc, "metadata", "name") != name {
					continue
				}
				if fileIdx >= 0 {
					return -1, -1, false, fmt.Errorf("deployment %s is defined more than once (%s, %s)", name, files[fileIdx].Path, f.Path)
				}
				fileIdx, docIdx = fi, di
			case "HorizontalPodAutoscaler":
				if scalarAt(doc, "spec", "scaleTargetRef", "kind") == "Deployment" &&
					scalarAt(doc, "spec", "scaleTargetRef", "name") == name {
					hpaManaged = true
				}
			}
		}
	}
	if fileIdx < 0 {
		return -1, -1, false, fmt.Errorf("no Deployment named %s in the workload folder", name)
	}
	return fileIdx, docIdx, hpaManaged, nil
}

// decodeAll returns the root mapping of every non-empty document in data.
func decodeAll(data []byte) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, err
		}
		if len(doc.Content) == 0 {
			docs = append(docs, nil)
			continue
		}
		docs = append(docs, doc.Content[0])
	}
}

func deploymentDoc(data []byte, docIdx int) (*yaml.Node, error) {
	docs, err := decodeAll(data)
	if err != nil {
		return nil, err
	}
	if docIdx >= len(docs) || docs[docIdx] == nil {
		return nil, fmt.Errorf("document %d disappeared after editing", docIdx)
	}
	return docs[docIdx], nil
}

// mappingValue returns the value node for key in a mapping node, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func lookup(n *yaml.Node, keys ...string) *yaml.Node {
	for _, k := range keys {
		n = mappingValue(n, k)
	}
	return n
}

func scalarAt(n *yaml.Node, keys ...string) string {
	if v := lookup(n, keys...); v != nil && v.Kind == yaml.ScalarNode {
		return v.Value
	}
	return ""
}

// lineSet splits data into lines without their terminators and remembers the line ending.
type lineSet struct {
	lines []string
	eol   string
}

func splitLines(data []byte) lineSet {
	s := string(data)
	eol := "\n"
	if strings.Contains(s, "\r\n") {
		eol = "\r\n"
	}
	return lineSet{lines: strings.Split(s, eol), eol: eol}
}

func (ls lineSet) bytes() []byte {
	return []byte(strings.Join(ls.lines, ls.eol))
}

// replaceScalar rewrites one scalar in place, keeping its quote style and any trailing comment.
func replaceScalar(data []byte, node *yaml.Node, value string) ([]byte, error) {
	if node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return nil, fmt.Errorf("block scalar at line %d is not supported", node.Line)
	}
	ls := splitLines(data)
	if node.Line < 1 || node.Line > len(ls.lines) {
		return nil, fmt.Errorf("line %d out of range", node.Line)
	}
	line := []rune(ls.lines[node.Line-1])
	start := node.Column - 1
	if start < 0 || start >= len(line) {
		return nil, fmt.Errorf("column %d out of range on line %d", node.Column, node.Line)
	}

	var end int // exclusive
	var inner, replacement string
	switch {
	case node.Style&yaml.DoubleQuotedStyle != 0:
		end = closingQuote(line, start, '"')
		replacement = `"` + value + `"`
	case node.Style&yaml.SingleQuotedStyle != 0:
		end = closingQuote(line, start, '\'')
		replacement = "'" + value + "'"
	default:
		end = len(line)
		for i := start; i < len(line); i++ {
			if line[i] == '#' && i > start && (line[i-1] == ' ' || line[i-1] == '\t') {
				end = i
				break
			}
		}
		for end > start && (line[end-1] == ' ' || line[end-1] == '\t') {
			end--
		}
		replacement = value
	}
	if end < 0 {
		return nil, fmt.Errorf("unterminated quoted scalar on line %d", node.Line)
	}
	inner = string(line[start:end])
	if node.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 {
		inner = inner[1 : len(inner)-1]
	}
	// Refuse anything that is not a simple one-line scalar, e.g. escapes or continuation lines.
	if inner != node.Value {
		return nil, fmt.Errorf("scalar on line %d spans lines or uses escapes; edit by hand", node.Line)
	}

	ls.lines[node.Line-1] = string(line[:start]) + replacement + string(line[end:])
	return ls.bytes(), nil
}

// closingQuote returns the index just past the quote that closes the scalar opened at start, or -1.
func closingQuote(line []rune, start int, quote rune) int {
	for i := start + 1; i < len(line); i++ {
		switch {
		case quote == '"' && line[i] == '\\':
			i++
		case quote == '\'' && line[i] == '\'' && i+1 < len(line) && line[i+1] == '\'':
			i++
		case line[i] == quote:
			return i + 1
		}
	}
	return -1
}

// insertKeys adds keys (nested, the last one holding value) as new lines after parent's last
// child, indented like parent's existing keys and two spaces per new level.
func insertKeys(data []byte, parent *yaml.Node, keys []string, value string, quote bool) ([]byte, error) {
	if parent.Kind != yaml.MappingNode || parent.Style&yaml.FlowStyle != 0 || len(parent.Content) == 0 {
		return nil, fmt.Errorf("cannot insert %q: parent is not a block mapping", keys[0])
	}
	indent := parent.Content[0].Column - 1
	ls := splitLines(data)

	last := maxLine(parent)
	// Continuation lines (block scalars, wrapped values) are more indented than the parent's keys.
	for last < len(ls.lines) {
		next := ls.lines[last]
		trimmed := strings.TrimLeft(next, " ")
		if strings.TrimSpace(next) == "" || len(next)-len(trimmed) <= indent || strings.HasPrefix(next, "---") {
			break
		}
		last++
	}

	if quote {
		value = `"` + value + `"`
	}
	added := make([]string, len(keys))
	for i, k := range keys {
		pad := strings.Repeat(" ", indent+2*i)
		if i == len(keys)-1 {
			added[i] = pad + k + ": " + value
		} else {
			added[i] = pad + k + ":"
		}
	}
	lines := make([]string, 0, len(ls.lines)+len(added))
	lines = append(lines, ls.lines[:last]...)
	lines = append(lines, added...)
	lines = append(lines, ls.lines[last:]...)
	ls.lines = lines
	return ls.bytes(), nil
}

// maxLine returns the highest source line of any node in the subtree.
func maxLine(n *yaml.Node) int {
	m := n.Line
	for _, c := range n.Content {
		if l := maxLine(c); l > m {
			m = l
		}
	}
	return m
}

// verifyPatch re-parses the patched file and checks every edit reads back.
func verifyPatch(data []byte, docIdx int, edits []fieldEdit) error {
	dep, err := deploymentDoc(data, docIdx)
	if err != nil {
		return err
	}
	for _, e := range edits {
		var got string
		switch e.Field {
		case "replicas":
			got = scalarAt(dep, "spec", "replicas")
		default:
			containers := lookup(dep, "spec", "template", "spec", "containers")
			if containers == nil || len(containers.Content) == 0 {
				return errors.New("containers missing")
			}
			got = scalarAt(containers.Content[0], "resources", "requests", e.Field)
		}
		if got != e.New {
			return fmt.Errorf("%s reads back as %q, want %q", e.Field, got, e.New)
		}
	}
	return nil
}
