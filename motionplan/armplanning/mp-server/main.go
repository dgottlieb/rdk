// Package main is a local web server for visualizing motion planning decisions.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	viz "github.com/viam-labs/motion-tools/client/client"

	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/motionplan"
	"go.viam.com/rdk/motionplan/armplanning"
	"go.viam.com/rdk/referenceframe"
	"go.viam.com/rdk/spatialmath"
)

const (
	rdkRoot          = "/home/dgottlieb/viam/rdk"
	planFilesRoot    = rdkRoot + "/mplans"
	renderFramePeriod = 5 * time.Millisecond
)

// ---- templates ----

var indexTmpl = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html>
<head>
<title>Motion Plan Files</title>
<style>
  body {
    background-color: azure;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    margin: 20px;
  }
  h1 { color: #333; }
  table {
    background-color: #D0EEFF;
    border-spacing: 8px;
    border: 1px solid black;
  }
  th, td {
    background-color: bisque;
    border: 1px solid black;
    padding: 4px 8px;
  }
  button {
    padding: 4px 8px;
    border: 1px solid black;
    background-color: #D0EEFF;
    cursor: pointer;
  }
</style>
</head>
<body>
<h1>Motion Plan Files</h1>
<table>
  <tr><th>File</th><th>Visualize</th><th>Details</th></tr>
  {{range .}}
  <tr>
    <td>{{.}}</td>
    <td><button onclick="renderStart('{{.}}')">Render State</button></td>
    <td><a href="/detail?file={{.}}">Details</a></td>
  </tr>
  {{end}}
</table>
<script>
function renderStart(file) {
  fetch('/render-start?file=' + encodeURIComponent(file))
    .then(r => { if (!r.ok) r.text().then(msg => alert('Error: ' + msg)); })
    .catch(err => alert('Error: ' + err));
}
</script>
</body>
</html>
`))

var detailTmpl = template.Must(template.New("detail").Parse(`<!DOCTYPE html>
<html>
<head>
<title>{{.File}} — Motion Plan Detail</title>
<style>
  body {
    background-color: azure;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    margin: 20px;
  }
  h1, h2 { color: #333; }
  table {
    background-color: #D0EEFF;
    border-spacing: 8px;
    border: 1px solid black;
  }
  th, td {
    background-color: bisque;
    border: 1px solid black;
    padding: 4px 8px;
    vertical-align: top;
  }
  button {
    padding: 4px 8px;
    border: 1px solid black;
    background-color: #D0EEFF;
    cursor: pointer;
  }
  pre {
    background-color: bisque;
    border: 1px solid black;
    padding: 8px;
    white-space: pre-wrap;
  }
  #result { margin-top: 16px; }
</style>
</head>
<body>
<h1>{{.File}}</h1>
<a href="/">← Back</a>

<h2>Motion Planning</h2>
<label>Timeout (seconds): <input id="timeout" type="number" min="0" step="1" value="0" style="width:6ch; padding:4px 8px; border:1px solid black;"></label>
&nbsp;<button onclick="runPlanning()">Do Motion Planning</button>
&nbsp;<button onclick="renderState()">Render Start State</button>
<div id="result"></div>

<h2>Frame System</h2>
<table>
  <tr><th>Frame</th><th>DoF</th><th>Parent</th></tr>
  {{range .Frames}}
  <tr>
    <td>{{.Name}}</td>
    <td>{{.DoF}}</td>
    <td>{{.Parent}}</td>
  </tr>
  {{end}}
</table>

<script>
function renderState() {
  fetch('/render-start?file=' + encodeURIComponent('{{.File}}'))
    .then(r => { if (!r.ok) r.text().then(msg => console.error('Render error: ' + msg)); })
    .catch(err => console.error('Render error: ' + err));
}

renderState();

let planAbortController = null;

function runPlanning() {
  if (planAbortController) {
    planAbortController.abort();
  }
  planAbortController = new AbortController();
  const div = document.getElementById('result');
  const timeout = document.getElementById('timeout').value;
  div.textContent = 'Running…';
  fetch('/plan/run?file=' + encodeURIComponent('{{.File}}') + '&timeout=' + encodeURIComponent(timeout),
        { signal: planAbortController.signal })
    .then(r => r.json())
    .then(data => {
      if (data.error) {
        div.innerHTML = '<pre style="color:#cc0000">Error: ' + data.error + '</pre>';
        return;
      }
      let html = '<p><strong>Steps:</strong> ' + data.steps +
                 ' &nbsp; <strong>Duration:</strong> ' + data.duration +
                 ' &nbsp; <strong>Goals processed:</strong> ' + data.goals_processed + '</p>';
      (data.per_goal || []).forEach((pg, goalIdx) => {
        html += '<h3>Goal ' + goalIdx + '</h3>';
        html += buildSolutionTable('{{.File}}', 'Valid solutions', pg.valid_solutions || [], false);
        html += buildSolutionTable('{{.File}}', 'Invalid solutions', pg.invalid_solutions || [], true);
        if (pg.constraint_failures_by_type && Object.keys(pg.constraint_failures_by_type).length) {
          html += '<h4>Constraint failures</h4><table><tr><th>Constraint</th><th>Count</th></tr>';
          for (const [k, v] of Object.entries(pg.constraint_failures_by_type)) {
            html += '<tr><td>' + escHtml(k) + '</td><td>' + v + '</td></tr>';
          }
          html += '</table>';
        }
      });
      div.innerHTML = html;
    })
    .catch(err => { if (err.name !== 'AbortError') div.textContent = 'Fetch error: ' + err; });
}

function buildSolutionTable(file, title, solutions, showError) {
  if (!solutions.length) return '';
  let html = '<h4>' + title + ' (' + solutions.length + ')</h4>';
  html += '<table><tr><th>Score</th><th>Inputs</th>';
  if (showError) html += '<th>Error</th>';
  html += '<th></th></tr>';
  for (const sn of solutions) {
    const inputStr = Object.entries(sn.inputs)
      .map(([f, vs]) => f + ': [' + vs.map(v => v.toFixed(4)).join(', ') + ']')
      .join('<br>');
    html += '<tr><td>' + sn.score.toFixed(4) + '</td><td><code>' + inputStr + '</code></td>';
    if (showError) html += '<td>' + escHtml(sn.check_path_error) + '</td>';
    html += '<td><button onclick=\'renderSolution(' + JSON.stringify(file) + ',' +
            JSON.stringify(sn.inputs) + ')\'>Render</button></td></tr>';
  }
  html += '</table>';
  return html;
}

function renderSolution(file, inputs) {
  fetch('/render-solution?file=' + encodeURIComponent(file), {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(inputs),
  }).then(r => { if (!r.ok) r.text().then(msg => console.error('Render error: ' + msg)); })
    .catch(err => console.error('Render error: ' + err));
}

function escHtml(s) {
  return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
}
</script>
</body>
</html>
`))

// ---- data types ----

type frameInfo struct {
	Name   string
	DoF    int
	Parent string
}

type detailData struct {
	File   string
	Frames []frameInfo
}

type planRunResult struct {
	Error          string          `json:"error,omitempty"`
	Steps          int             `json:"steps,omitempty"`
	Duration       string          `json:"duration,omitempty"`
	GoalsProcessed int             `json:"goals_processed,omitempty"`
	Partial        bool            `json:"partial,omitempty"`
	PartialError   string          `json:"partial_error,omitempty"`
	PerGoal        []perGoalResult `json:"per_goal,omitempty"`
}

type perGoalResult struct {
	ValidSolutions           []solutionNodeResult `json:"valid_solutions,omitempty"`
	InvalidSolutions         []solutionNodeResult `json:"invalid_solutions,omitempty"`
	ConstraintFailuresByType map[string]int       `json:"constraint_failures_by_type,omitempty"`
}

type solutionNodeResult struct {
	Score          float64              `json:"score"`
	CheckPathError string               `json:"check_path_error,omitempty"`
	Inputs         map[string][]float64 `json:"inputs"`
}

func linearInputsToFloats(li *referenceframe.LinearInputs) map[string][]float64 {
	out := make(map[string][]float64)
	for frameName, inputs := range li.Items() {
		if len(inputs) == 0 {
			continue
		}
		floats := make([]float64, len(inputs))
		copy(floats, inputs)
		out[frameName] = floats
	}
	return out
}

func floatsToLinearInputs(data map[string][]float64) *referenceframe.LinearInputs {
	li := referenceframe.NewLinearInputs()
	for frameName, floats := range data {
		li.Put(frameName, floats)
	}
	return li
}

// ---- helpers ----

func findPlanFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Ext(path) == ".json" {
			rel, err := filepath.Rel(rdkRoot, path)
			if err != nil {
				return err
			}
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}

func buildFrameInfo(fs *referenceframe.FrameSystem) []frameInfo {
	var frames []frameInfo
	for _, name := range fs.FrameNames() {
		frame := fs.Frame(name)
		parentName := ""
		if parent, err := fs.Parent(frame); err == nil && parent != nil {
			parentName = parent.Name()
		}
		frames = append(frames, frameInfo{
			Name:   name,
			DoF:    len(frame.DoF()),
			Parent: parentName,
		})
	}
	sort.Slice(frames, func(idx, jdx int) bool {
		if frames[idx].DoF != frames[jdx].DoF {
			return frames[idx].DoF > frames[jdx].DoF
		}
		return frames[idx].Name < frames[jdx].Name
	})
	return frames
}

func drawGoalPoses(req *armplanning.PlanRequest) error {
	var goalPoses []spatialmath.Pose
	for _, goalPlanState := range req.Goals {
		poses, err := goalPlanState.ComputePoses(context.Background(), req.FrameSystem)
		if err != nil {
			return err
		}
		for _, poseValue := range poses {
			poseInWorldFrame := poseValue.Transform(
				referenceframe.NewPoseInFrame(
					req.FrameSystem.World().Name(),
					spatialmath.NewZeroPose())).(*referenceframe.PoseInFrame)
			goalPoses = append(goalPoses, poseInWorldFrame.Pose())
		}
	}
	return viz.DrawPoses(goalPoses, []string{"blue"}, true)
}

func renderState(relPath string) error {
	req, err := armplanning.ReadRequestFromFile(filepath.Join(rdkRoot, relPath))
	if err != nil {
		return fmt.Errorf("reading plan file: %w", err)
	}
	startInputs := req.StartState.Configuration()
	if err := viz.RemoveAllSpatialObjects(); err != nil {
		return fmt.Errorf("clearing visualizer: %w", err)
	}
	if err := viz.DrawWorldState(req.WorldState, req.FrameSystem, startInputs); err != nil {
		return fmt.Errorf("drawing world state: %w", err)
	}
	if err := viz.DrawFrameSystem(req.FrameSystem, startInputs); err != nil {
		return fmt.Errorf("drawing frame system: %w", err)
	}
	if err := drawGoalPoses(req); err != nil {
		return fmt.Errorf("drawing goal poses: %w", err)
	}
	return nil
}

func visualizePlan(req *armplanning.PlanRequest, plan motionplan.Plan) error {
	startInputs := req.StartState.Configuration()
	if err := viz.RemoveAllSpatialObjects(); err != nil {
		return err
	}
	if err := viz.DrawWorldState(req.WorldState, req.FrameSystem, startInputs); err != nil {
		return err
	}
	if err := viz.DrawFrameSystem(req.FrameSystem, startInputs); err != nil {
		return err
	}
	if err := drawGoalPoses(req); err != nil {
		return err
	}
	for idx := range plan.Path() {
		if idx > 0 {
			midPoints, err := motionplan.InterpolateSegmentFS(
				&motionplan.SegmentFS{
					StartConfiguration: plan.Trajectory()[idx-1].ToLinearInputs(),
					EndConfiguration:   plan.Trajectory()[idx].ToLinearInputs(),
					FS:                 req.FrameSystem,
				}, 2)
			if err != nil {
				return err
			}
			for _, mp := range midPoints {
				if err := viz.DrawFrameSystem(req.FrameSystem, mp.ToFrameSystemInputs()); err != nil {
					return err
				}
				time.Sleep(renderFramePeriod)
			}
		}
		if err := viz.DrawFrameSystem(req.FrameSystem, plan.Trajectory()[idx]); err != nil {
			return err
		}
		time.Sleep(renderFramePeriod)
	}
	return nil
}

// ---- handlers ----

func handleIndex(logger logging.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		files, err := findPlanFiles(planFilesRoot)
		if err != nil {
			http.Error(w, fmt.Sprintf("scan error: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := indexTmpl.Execute(w, files); err != nil {
			logger.Errorf("rendering index: %v", err)
		}
	}
}

func handleDetail(logger logging.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file := r.URL.Query().Get("file")
		if file == "" {
			http.Error(w, "missing file parameter", http.StatusBadRequest)
			return
		}
		req, err := armplanning.ReadRequestFromFile(filepath.Join(rdkRoot, file))
		if err != nil {
			http.Error(w, fmt.Sprintf("reading plan file: %v", err), http.StatusInternalServerError)
			return
		}
		data := detailData{
			File:   file,
			Frames: buildFrameInfo(req.FrameSystem),
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := detailTmpl.Execute(w, data); err != nil {
			logger.Errorf("rendering detail: %v", err)
		}
	}
}

func handlePlanRun(logger logging.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file := r.URL.Query().Get("file")
		if file == "" {
			http.Error(w, "missing file parameter", http.StatusBadRequest)
			return
		}

		req, err := armplanning.ReadRequestFromFile(filepath.Join(rdkRoot, file))
		if err != nil {
			writeJSON(w, planRunResult{Error: err.Error()})
			return
		}

		armplanning.ClearSeedCache()

		if req.PlannerOptions == nil {
			req.PlannerOptions = armplanning.NewBasicPlannerOptions()
		}
		req.PlannerOptions.CollectSolutionDiagnostics = true
		if timeoutStr := r.URL.Query().Get("timeout"); timeoutStr != "" {
			if secs, err := strconv.ParseFloat(timeoutStr, 64); err == nil && secs > 0 {
				req.PlannerOptions.Timeout = secs
			}
		}

		plan, meta, err := armplanning.PlanMotion(r.Context(), logger, req)
		if err != nil {
			writeJSON(w, planRunResult{Error: err.Error()})
			return
		}

		if vizErr := visualizePlan(req, plan); vizErr != nil {
			logger.Warnf("visualization failed (motion-tools server may not be running): %v", vizErr)
		}

		result := planRunResult{
			Steps:          len(plan.Path()),
			Duration:       meta.Duration.String(),
			GoalsProcessed: meta.GoalsProcessed,
			Partial:        meta.Partial,
		}
		if meta.PartialError != nil {
			result.PartialError = meta.PartialError.Error()
		}
		for _, pg := range meta.PerGoal {
			pgResult := perGoalResult{
				ConstraintFailuresByType: pg.ConstraintFailuresByType,
			}
			for _, sn := range pg.SolutionNodes {
				row := solutionNodeResult{
					Score:  sn.Score,
					Inputs: linearInputsToFloats(sn.Inputs),
				}
				if sn.CheckPathError != nil {
					row.CheckPathError = sn.CheckPathError.Error()
					pgResult.InvalidSolutions = append(pgResult.InvalidSolutions, row)
				} else {
					pgResult.ValidSolutions = append(pgResult.ValidSolutions, row)
				}
			}
			result.PerGoal = append(result.PerGoal, pgResult)
		}

		writeJSON(w, result)
	}
}

func handleRenderSolution(logger logging.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file := r.URL.Query().Get("file")
		if file == "" {
			http.Error(w, "missing file parameter", http.StatusBadRequest)
			return
		}
		req, err := armplanning.ReadRequestFromFile(filepath.Join(rdkRoot, file))
		if err != nil {
			http.Error(w, fmt.Sprintf("reading plan file: %v", err), http.StatusInternalServerError)
			return
		}
		var inputFloats map[string][]float64
		if err := json.NewDecoder(r.Body).Decode(&inputFloats); err != nil {
			http.Error(w, fmt.Sprintf("decoding inputs: %v", err), http.StatusBadRequest)
			return
		}
		li := floatsToLinearInputs(inputFloats)
		startInputs := req.StartState.Configuration()
		if err := viz.RemoveAllSpatialObjects(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := viz.DrawWorldState(req.WorldState, req.FrameSystem, startInputs); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := viz.DrawFrameSystem(req.FrameSystem, li.ToFrameSystemInputs()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := drawGoalPoses(req); err != nil {
			logger.Warnf("drawing goal poses: %v", err)
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleRenderStart(logger logging.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		file := r.URL.Query().Get("file")
		if file == "" {
			http.Error(w, "missing file parameter", http.StatusBadRequest)
			return
		}
		if err := renderState(file); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "Rendered start state for %s", file)
	}
}

// ---- main ----

func main() {
	logger := logging.NewLogger("mp-server")

	http.HandleFunc("/", handleIndex(logger))
	http.HandleFunc("/detail", handleDetail(logger))
	http.HandleFunc("/plan/run", handlePlanRun(logger))
	http.HandleFunc("/render-start", handleRenderStart(logger))
	http.HandleFunc("/render-solution", handleRenderSolution(logger))

	addr := "localhost:8080"
	logger.Infof("listening on http://%s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil { //nolint:gosec
		logger.Fatal(err)
	}
}
