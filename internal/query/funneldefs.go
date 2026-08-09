package query

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/var-raphael/gnat/internal/storage"
)

type funnelStepDef struct {
	EventName string `json:"event_name"`
	Label     string `json:"label"`
}

type funnelDef struct {
	ID          uint            `json:"id"`
	Name        string          `json:"name"`
	Steps       []funnelStepDef `json:"steps"`
	WindowHours int             `json:"window_hours"`
}

func toFunnelDef(f storage.Funnel) funnelDef {
	var steps []funnelStepDef
	json.Unmarshal([]byte(f.Steps), &steps)
	return funnelDef{ID: f.ID, Name: f.Name, Steps: steps, WindowHours: f.WindowHours}
}

// FunnelDefsHandler handles GET (list) and POST (create) on /api/funnels.
func FunnelDefsHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			var funnels []storage.Funnel
			if err := db.Order("created_at").Find(&funnels).Error; err != nil {
				http.Error(w, "query failed", http.StatusInternalServerError)
				return
			}
			defs := make([]funnelDef, 0, len(funnels))
			for _, f := range funnels {
				defs = append(defs, toFunnelDef(f))
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(defs)

		case http.MethodPost:
			var in funnelDef
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, "invalid json body", http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(in.Name) == "" {
				http.Error(w, "name is required", http.StatusBadRequest)
				return
			}
			if len(in.Steps) < 2 {
				http.Error(w, "at least 2 steps are required", http.StatusBadRequest)
				return
			}
			if in.WindowHours <= 0 {
				in.WindowHours = 168
			}

			stepsJSON, err := json.Marshal(in.Steps)
			if err != nil {
				http.Error(w, "invalid steps", http.StatusBadRequest)
				return
			}

			funnel := storage.Funnel{
				SiteID:      1,
				Name:        in.Name,
				Steps:       string(stepsJSON),
				WindowHours: in.WindowHours,
			}
			if err := db.Create(&funnel).Error; err != nil {
				http.Error(w, "failed to save funnel", http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(toFunnelDef(funnel))

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// FunnelDefHandler handles PUT (update) and DELETE on /api/funnels/{id}.
func FunnelDefHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := funnelIDFromPath(r.URL.Path)
		if err != nil {
			http.Error(w, "invalid funnel id", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodPut:
			var in funnelDef
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, "invalid json body", http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(in.Name) == "" {
				http.Error(w, "name is required", http.StatusBadRequest)
				return
			}
			if len(in.Steps) < 2 {
				http.Error(w, "at least 2 steps are required", http.StatusBadRequest)
				return
			}
			if in.WindowHours <= 0 {
				in.WindowHours = 168
			}

			stepsJSON, err := json.Marshal(in.Steps)
			if err != nil {
				http.Error(w, "invalid steps", http.StatusBadRequest)
				return
			}

			var funnel storage.Funnel
			if err := db.First(&funnel, id).Error; err != nil {
				http.Error(w, "funnel not found", http.StatusNotFound)
				return
			}
			funnel.Name = in.Name
			funnel.Steps = string(stepsJSON)
			funnel.WindowHours = in.WindowHours
			if err := db.Save(&funnel).Error; err != nil {
				http.Error(w, "failed to update funnel", http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(toFunnelDef(funnel))

		case http.MethodDelete:
			if err := db.Delete(&storage.Funnel{}, id).Error; err != nil {
				http.Error(w, "failed to delete funnel", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func funnelIDFromPath(path string) (uint, error) {
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	idStr := parts[len(parts)-1]
	id, err := strconv.ParseUint(idStr, 10, 64)
	return uint(id), err
}

type computedFunnel struct {
	ID    uint                `json:"id"`
	Name  string              `json:"name"`
	Steps []funnelStepResult2 `json:"steps"`
}

type funnelStepResult2 struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// FunnelResultsHandler returns GET /api/stats/funnels?from=...&to=...
// Loads every saved funnel and computes its current results.
func FunnelResultsHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		from, to, err := parseRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var funnels []storage.Funnel
		if err := db.Order("created_at").Find(&funnels).Error; err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}

		results := make([]computedFunnel, 0, len(funnels))
		for _, f := range funnels {
			var steps []funnelStepDef
			if err := json.Unmarshal([]byte(f.Steps), &steps); err != nil || len(steps) < 2 {
				continue
			}
			eventNames := make([]string, len(steps))
			for i, s := range steps {
				eventNames[i] = s.EventName
			}

			window := time.Duration(f.WindowHours) * time.Hour
			stepResults, err := computeFunnelStaged(db, eventNames, from, to, window)
			if err != nil {
				http.Error(w, "query failed", http.StatusInternalServerError)
				return
			}

			out := make([]funnelStepResult2, len(stepResults))
			for i, sr := range stepResults {
				label := steps[i].Label
				if label == "" {
					label = sr.Step
				}
				out[i] = funnelStepResult2{Label: label, Count: sr.Count}
			}

			results = append(results, computedFunnel{ID: f.ID, Name: f.Name, Steps: out})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	}
}
