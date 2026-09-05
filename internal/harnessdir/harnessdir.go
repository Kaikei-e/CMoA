// Package harnessdir reads a rendered harness directory: the tree a layer
// above materialises from the harness edits that are in force, and hands to
// `cmoa propose --harness <dir>`.
//
// The layout is fixed and small. Everything else in the tree is recorded
// but not rendered, so a surface CMoA cannot inject yet is still visible in
// the digest:
//
//	system-prompt.md          optional; appended verbatim to the system contract
//	memory/<file>.md          zero or more; the notes, in path order
//	skills/<name>/SKILL.md    zero or more; name and description only
//
// CMoA hashes the tree itself rather than trusting the renderer's own
// manifest: the digest in run.json is what CMoA read, so the two can be
// compared. Two paths are excluded — `.git` (whether a directory or the
// one-line file a worktree leaves) and its contents, which is not harness
// content, and a top-level `render.json`, the renderer's own manifest,
// which cannot contain its own digest. The digest is over files: an empty
// directory does not reach it, which is why an empty skill directory is
// refused outright rather than left to the comparison.
package harnessdir

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Kaikei-e/CMoA/internal/prompt"
)

// ErrNotFound is wrapped when the directory is absent or is not a directory.
var ErrNotFound = errors.New("harnessdir: not a directory")

// File is one file of the tree, by its slash-separated path relative to the
// directory root.
type File struct {
	Path   string
	SHA256 string
}

// Dir is what a run read: the tree, its digest, and the rendered harness.
// Path is absolute, as the vault path in the same record is.
type Dir struct {
	Path       string
	TreeSHA256 string
	Files      []File
	Harness    prompt.Harness
}

// The names of the layout. Nothing else in CMoA spells them.
const (
	systemPromptFile = "system-prompt.md"
	memoryDir        = "memory"
	skillsDir        = "skills"
	skillFile        = "SKILL.md"
	manifestFile     = "render.json"
	gitDir           = ".git"
)

// skillNamePattern bounds a skill's name. The name is a directory name,
// which a filesystem lets hold a newline, and it is rendered as one line of
// a list a proposer reads: an unbounded name can write list entries of its
// own. The harness is machine-proposed, so the alphabet is restricted for
// the same reason a proposer id is.
var skillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Load reads dir. A directory holding none of the three surfaces is valid
// and renders the prompt a run without a harness renders.
func Load(dir string) (*Dir, error) {
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, dir)
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	files, dirs, err := walk(dir)
	if err != nil {
		return nil, err
	}
	h, err := render(dir, files, dirs)
	if err != nil {
		return nil, err
	}
	return &Dir{Path: dir, TreeSHA256: treeSHA256(files), Files: files, Harness: h}, nil
}

// walk hashes every regular file under dir and lists every directory, both
// in path order. Directories are listed because an empty one is invisible
// to the digest and still means something: a skill that was meant to be
// added.
func walk(dir string) (files []File, dirs []string, err error) {
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("harnessdir: %s: %w", dir, err)
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.Name() == gitDir {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil // the one-line gitfile a worktree or submodule leaves
		}
		if d.IsDir() {
			dirs = append(dirs, rel)
			return nil
		}
		if rel == manifestFile {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("harnessdir: %s is not a regular file", rel)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("harnessdir: read %s: %w", rel, err)
		}
		if !utf8.Valid(b) {
			return fmt.Errorf("harnessdir: %s is not valid UTF-8; proposers only see text", rel)
		}
		sum := sha256.Sum256(b)
		files = append(files, File{Path: rel, SHA256: hex.EncodeToString(sum[:])})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	sort.Strings(dirs)
	return files, dirs, nil
}

// treeSHA256 digests the manifest "<path>\n<sha256>\n" per file, in path
// order. It is the whole tree in one number, so a caller can compare what
// CMoA read against what a renderer says it wrote.
func treeSHA256(files []File) string {
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s\n%s\n", f.Path, f.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// render turns the tree into the value the prompt templates take. Only the
// three surfaces are read; the file list already carries the rest.
func render(dir string, files []File, dirs []string) (prompt.Harness, error) {
	var h prompt.Harness
	for _, f := range files {
		switch {
		case f.Path == systemPromptFile:
			b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
			if err != nil {
				return h, err
			}
			h.SystemAppendix = strings.TrimRight(string(b), "\n")
		case strings.HasPrefix(f.Path, memoryDir+"/") && strings.HasSuffix(f.Path, ".md"):
			b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
			if err != nil {
				return h, err
			}
			body := strings.TrimRight(string(b), "\n")
			if strings.TrimSpace(body) == "" {
				continue // an empty note says nothing; rendering it says nothing louder
			}
			h.Notes = append(h.Notes, prompt.Note{Path: f.Path, Body: body})
		case strings.HasPrefix(f.Path, skillsDir+"/") && path.Base(f.Path) == skillFile:
			name := strings.TrimSuffix(strings.TrimPrefix(f.Path, skillsDir+"/"), "/"+skillFile)
			if strings.Contains(name, "/") {
				return h, fmt.Errorf("harnessdir: %s: a skill is %s/<name>/%s", f.Path, skillsDir, skillFile)
			}
			if err := checkSkillName(name); err != nil {
				return h, err
			}
			b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Path)))
			if err != nil {
				return h, err
			}
			desc := Description(string(b))
			if desc == "" {
				return h, fmt.Errorf("harnessdir: skill %q has no description: %s needs a frontmatter description or a first line", name, f.Path)
			}
			h.Skills = append(h.Skills, prompt.Skill{Name: name, Description: desc})
		}
	}
	if err := checkSkillDirs(files, dirs, h.Skills); err != nil {
		return prompt.Harness{}, err
	}
	// Notes and skills come out of walk in path order, which is the order
	// they are rendered in.
	return h, nil
}

// checkSkillDirs refuses a skills/<name>/ that has no SKILL.md file, empty
// or not. Such a directory is a skill the renderer meant to add and CMoA
// would silently not show, which would make the edit measure as a no-op for
// the wrong reason. It is checked against the directories as well as the
// files because an empty directory reaches neither the digest nor the file
// list.
func checkSkillDirs(files []File, dirs []string, skills []prompt.Skill) error {
	named := map[string]bool{}
	for _, s := range skills {
		named[s.Name] = true
	}
	paths := make([]string, 0, len(files)+len(dirs))
	paths = append(paths, dirs...)
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	sort.Strings(paths)
	for _, p := range paths {
		rest, ok := strings.CutPrefix(p, skillsDir+"/")
		if !ok {
			continue
		}
		name, _, nested := strings.Cut(rest, "/")
		if !nested && !isDir(dirs, p) {
			continue // a file directly under skills/, such as a .gitkeep
		}
		if err := checkSkillName(name); err != nil {
			return err
		}
		if !named[name] {
			return fmt.Errorf("harnessdir: %s/%s has no %s file", skillsDir, name, skillFile)
		}
	}
	return nil
}

func isDir(dirs []string, p string) bool {
	for _, d := range dirs {
		if d == p {
			return true
		}
	}
	return false
}

func checkSkillName(name string) error {
	if !skillNamePattern.MatchString(name) {
		return fmt.Errorf("harnessdir: skill name %q must match %s", name, skillNamePattern)
	}
	return nil
}

// Description is a skill's one line: the frontmatter `description:` when
// there is one, otherwise the first line of the body that is neither blank
// nor a heading. Newlines are folded to spaces — the listing is one line
// per skill.
func Description(content string) string {
	lines := strings.Split(content, "\n")
	body := 0
	if len(lines) > 0 && strings.TrimRight(lines[0], "\r ") == "---" {
		end := -1
		for i := 1; i < len(lines); i++ {
			t := strings.TrimRight(lines[i], "\r ")
			if t == "---" || t == "..." {
				end = i
				break
			}
		}
		if end > 0 {
			if d := frontmatterDescription(lines[1:end]); d != "" {
				return d
			}
			body = end + 1
		}
	}
	for i := body; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || t == "---" || strings.HasPrefix(t, "#") {
			continue
		}
		return fold(t)
	}
	return ""
}

// frontmatterDescription reads a top-level `description:` out of the
// frontmatter lines. A block scalar (`|`, `>`) and a value continued on the
// following indented lines are both folded into one line; anything more
// than that is more YAML than a description needs.
func frontmatterDescription(lines []string) string {
	for i, l := range lines {
		rest, ok := strings.CutPrefix(l, "description:")
		if !ok { // indented keys belong to another mapping
			continue
		}
		v := strings.TrimSpace(rest)
		if v == "" || v == "|" || v == ">" || strings.HasPrefix(v, "|") || strings.HasPrefix(v, ">") {
			var parts []string
			for j := i + 1; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == "" {
					continue
				}
				if !strings.HasPrefix(lines[j], " ") && !strings.HasPrefix(lines[j], "\t") {
					break
				}
				parts = append(parts, strings.TrimSpace(lines[j]))
			}
			return fold(strings.Join(parts, " "))
		}
		return fold(unquote(v))
	}
	return ""
}

func unquote(v string) string {
	if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
		return strings.Trim(v, `"`)
	}
	if len(v) >= 2 && strings.HasPrefix(v, `'`) && strings.HasSuffix(v, `'`) {
		return strings.ReplaceAll(strings.Trim(v, `'`), "''", "'")
	}
	return v
}

func fold(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}
