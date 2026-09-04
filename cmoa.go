// Package cmoa is the public face of CMoA, a selection-type Mixture-of-Agents
// runtime. The package exports exactly what the layer above (uzushio) needs
// to lower into DocDag vocabulary: the harness surfaces a self-improvement
// loop may edit and the autonomy each surface is granted. Everything that
// runs — the router, the proposer pool, selection, traces — lives under
// internal/ and is reached through the cmoa command.
//
// The seven surfaces follow the component list of Agentic Harness
// Engineering (arXiv 2604.25850). The verifier, the tracer and the model
// configuration are not surfaces: a loop reads them, it never writes them.
package cmoa

import "fmt"

// Surface is one harness component a self-improvement loop may propose an
// edit for. The string form is the vocabulary uzushio writes into the
// `component` and `touches` fields of an edit document.
type Surface string

// The seven editable surfaces, in the order they are listed everywhere.
const (
	SurfaceSystemPrompt       Surface = "system-prompt"
	SurfaceToolDescription    Surface = "tool-description"
	SurfaceToolImplementation Surface = "tool-implementation"
	SurfaceMiddleware         Surface = "middleware"
	SurfaceSkill              Surface = "skill"
	SurfaceSubagentConfig     Surface = "subagent-config"
	SurfaceMemory             Surface = "memory"
)

// Autonomy is how far an edit to a surface may travel without a person.
// The names are provisional (design §9) and are promoted by superseding the
// record that declares them, never by editing it.
type Autonomy string

const (
	// AutonomyAutoAccept: a proposal that passes the held-out split is
	// accepted without a person in the loop.
	AutonomyAutoAccept Autonomy = "auto-accept"
	// AutonomyHumanApproval: a proposal is validated, then a person accepts
	// or rejects it.
	AutonomyHumanApproval Autonomy = "human-approval"
	// AutonomyProposeOnly: a proposal is recorded and validated but never
	// applied by the loop, whoever approves it.
	AutonomyProposeOnly Autonomy = "propose-only"
)

// ReadOnlyComponent is a part of the harness the loop reads and must not
// write. They are listed so uzushio can refuse an edit that names one.
type ReadOnlyComponent string

const (
	ReadOnlyVerifier    ReadOnlyComponent = "verifier"
	ReadOnlyTracer      ReadOnlyComponent = "tracer"
	ReadOnlyModelConfig ReadOnlyComponent = "model-config"
)

var allSurfaces = [...]Surface{
	SurfaceSystemPrompt,
	SurfaceToolDescription,
	SurfaceToolImplementation,
	SurfaceMiddleware,
	SurfaceSkill,
	SurfaceSubagentConfig,
	SurfaceMemory,
}

var autonomyOf = map[Surface]Autonomy{
	SurfaceMemory:             AutonomyAutoAccept,
	SurfaceSkill:              AutonomyAutoAccept,
	SurfaceToolDescription:    AutonomyHumanApproval,
	SurfaceMiddleware:         AutonomyHumanApproval,
	SurfaceSubagentConfig:     AutonomyHumanApproval,
	SurfaceSystemPrompt:       AutonomyHumanApproval,
	SurfaceToolImplementation: AutonomyProposeOnly,
}

// AllSurfaces returns the seven surfaces in canonical order. The slice is a
// fresh copy; callers may sort or filter it.
func AllSurfaces() []Surface {
	out := make([]Surface, len(allSurfaces))
	copy(out, allSurfaces[:])
	return out
}

// AllSurfaceNames is AllSurfaces as strings, the form a DocDag `one_of`
// field takes.
func AllSurfaceNames() []string {
	out := make([]string, len(allSurfaces))
	for i, s := range allSurfaces {
		out[i] = string(s)
	}
	return out
}

// EditableSurfaces returns the surfaces whose autonomy is not
// AutonomyProposeOnly: the vocabulary an edit's `touches` must stay inside.
func EditableSurfaces() []Surface {
	var out []Surface
	for _, s := range allSurfaces {
		if s.Autonomy() != AutonomyProposeOnly {
			out = append(out, s)
		}
	}
	return out
}

// ReadOnlyComponents returns the three components a loop never writes.
func ReadOnlyComponents() []ReadOnlyComponent {
	return []ReadOnlyComponent{ReadOnlyVerifier, ReadOnlyTracer, ReadOnlyModelConfig}
}

// Autonomy returns the autonomy granted to the surface. It panics on a
// value that is not one of the seven constants, because such a value cannot
// have been produced by this package.
func (s Surface) Autonomy() Autonomy {
	a, ok := autonomyOf[s]
	if !ok {
		panic(fmt.Sprintf("cmoa: %q is not a Surface", string(s)))
	}
	return a
}

// ParseSurface returns the Surface named by s, or an error naming the
// vocabulary. It is the only way to obtain a Surface from outside input.
func ParseSurface(s string) (Surface, error) {
	for _, known := range allSurfaces {
		if string(known) == s {
			return known, nil
		}
	}
	return "", fmt.Errorf("cmoa: %q is not a surface; one of %v", s, AllSurfaceNames())
}
