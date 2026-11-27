package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/t0mer/mashov2go/internal/settings"
)

// generalView is the general settings as the API shows them. Durations are
// strings such as "5m" or "8760h".
type generalView struct {
	Timezone          string `json:"timezone"`
	MashovMinGap      string `json:"mashovMinGap"`
	MashovProfile     string `json:"mashovProfile"`
	SyncFastInterval  string `json:"syncFastInterval"`
	SyncFullInterval  string `json:"syncFullInterval"`
	SyncNightInterval string `json:"syncNightInterval"`
	ActiveHours       string `json:"activeHours"`
	PlanDefaultTime   string `json:"planDefaultTime"`
	EventsRetention   string `json:"eventsRetention"`
	PreserveUnread    bool   `json:"preserveUnread"`
	ShabbatMode       bool   `json:"shabbatMode"`
}

func viewGeneral(s settings.Settings) generalView {
	return generalView{
		Timezone: s.Timezone, MashovMinGap: s.MashovMinGap.String(), MashovProfile: s.MashovProfile,
		SyncFastInterval: s.SyncFastInterval.String(), SyncFullInterval: s.SyncFullInterval.String(),
		SyncNightInterval: s.SyncNightInterval.String(), ActiveHours: s.ActiveHours,
		PlanDefaultTime: s.PlanDefaultTime, EventsRetention: s.EventsRetention.String(),
		PreserveUnread: s.PreserveUnread, ShabbatMode: s.ShabbatMode,
	}
}

func (s *Server) settingsRoutes(mux *http.ServeMux) {
	s.handle(mux, "GET /api/v1/settings/general", s.authed("read", http.HandlerFunc(s.getGeneral)))
	s.handle(mux, "PATCH /api/v1/settings/general", s.authed("admin", http.HandlerFunc(s.patchGeneral)))
}

func (s *Server) getGeneral(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, viewGeneral(s.Settings.Get()))
}

// patchGeneral applies the sent fields. Durations are parsed first; the settings
// service then validates the whole result and rejects anything out of range.
func (s *Server) patchGeneral(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Timezone          *string `json:"timezone"`
		MashovMinGap      *string `json:"mashovMinGap"`
		MashovProfile     *string `json:"mashovProfile"`
		SyncFastInterval  *string `json:"syncFastInterval"`
		SyncFullInterval  *string `json:"syncFullInterval"`
		SyncNightInterval *string `json:"syncNightInterval"`
		ActiveHours       *string `json:"activeHours"`
		PlanDefaultTime   *string `json:"planDefaultTime"`
		EventsRetention   *string `json:"eventsRetention"`
		PreserveUnread    *bool   `json:"preserveUnread"`
		ShabbatMode       *bool   `json:"shabbatMode"`
	}
	if !decode(w, r, &in) {
		return
	}
	durations := map[string]*string{
		"mashovMinGap": in.MashovMinGap, "syncFastInterval": in.SyncFastInterval,
		"syncFullInterval": in.SyncFullInterval, "syncNightInterval": in.SyncNightInterval,
		"eventsRetention": in.EventsRetention,
	}
	parsed := map[string]time.Duration{}
	for name, v := range durations {
		if v == nil {
			continue
		}
		d, err := time.ParseDuration(*v)
		if err != nil {
			problem(w, http.StatusBadRequest, "Bad request", name+" must be a duration such as 5m or 8760h")
			return
		}
		parsed[name] = d
	}
	next, err := s.Settings.Update(r.Context(), func(c *settings.Settings) {
		if in.Timezone != nil {
			c.Timezone = *in.Timezone
		}
		if in.MashovProfile != nil {
			c.MashovProfile = *in.MashovProfile
		}
		if in.ActiveHours != nil {
			c.ActiveHours = *in.ActiveHours
		}
		if in.PlanDefaultTime != nil {
			c.PlanDefaultTime = *in.PlanDefaultTime
		}
		if in.PreserveUnread != nil {
			c.PreserveUnread = *in.PreserveUnread
		}
		if in.ShabbatMode != nil {
			c.ShabbatMode = *in.ShabbatMode
		}
		if d, ok := parsed["mashovMinGap"]; ok {
			c.MashovMinGap = d
		}
		if d, ok := parsed["syncFastInterval"]; ok {
			c.SyncFastInterval = d
		}
		if d, ok := parsed["syncFullInterval"]; ok {
			c.SyncFullInterval = d
		}
		if d, ok := parsed["syncNightInterval"]; ok {
			c.SyncNightInterval = d
		}
		if d, ok := parsed["eventsRetention"]; ok {
			c.EventsRetention = d
		}
	})
	if errors.Is(err, settings.ErrInvalid) {
		problem(w, http.StatusBadRequest, "Bad request", err.Error())
		return
	}
	if err != nil {
		problem(w, http.StatusInternalServerError, "Internal error", "saving settings failed")
		return
	}
	writeJSON(w, http.StatusOK, viewGeneral(next))
}
