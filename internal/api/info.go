package api

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"

	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/plugin"
)

// Info is GET /info (api/openapi.json, Info).
type Info struct {
	Vendor   string            `json:"vendor"`
	Version  string            `json:"version"`
	Commit   string            `json:"commit,omitempty"`
	Date     string            `json:"date,omitempty"`
	Protocol int               `json:"protocol"`
	Auth     string            `json:"auth"`
	Host     map[string]any    `json:"host"`
	UI       map[string]string `json:"ui,omitempty"`
	Features []string          `json:"features"`
}

// Features of this build that /info lists.
const (
	FeatureUI     = "ui"
	FeatureWS     = "ws"
	FeatureSocket = "socket"
)

func (s *Server) getInfo(w http.ResponseWriter, r *http.Request) error {
	st, err := s.load()
	if err != nil {
		return err
	}
	info := Info{
		Vendor: s.opts.Build.Vendor, Version: s.opts.Build.Version, Commit: s.opts.Build.Commit,
		Date: s.opts.Build.Date, Protocol: s.opts.Build.Protocol, Auth: s.opts.Auth,
		Host: st.Host(), UI: st.UI(), Features: []string{},
	}
	if len(info.UI) == 0 {
		info.UI = nil
	}
	if s.ui.built() {
		info.Features = append(info.Features, FeatureUI)
	}
	if s.hasWS() {
		info.Features = append(info.Features, FeatureWS)
	}
	if s.socket.Load() {
		info.Features = append(info.Features, FeatureSocket)
	}
	s.writeJSON(w, r, http.StatusOK, info)
	return nil
}

// hasWS reports whether the WebSocket is available; the WebSocket work package sets it.
func (s *Server) hasWS() bool { return s.ws.Load() }

// SetWebSocket records that the WebSocket is available (feature "ws" of /info).
func (s *Server) SetWebSocket(on bool) { s.ws.Store(on) }

func (s *Server) listPlugins(w http.ResponseWriter, r *http.Request) error {
	withManifest := false
	if raw := r.URL.Query().Get("manifest"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return invalid("manifest must be true or false", fieldError{Pointer: "/manifest", Code: "type", Message: "not a boolean"})
		}
		withManifest = v
	}
	st, err := s.load()
	if err != nil {
		return err
	}
	plugins := st.Evaluate()
	sort.SliceStable(plugins, func(i, j int) bool { return plugins[i].ID < plugins[j].ID })
	out := make([]core.PluginDetail, 0, len(plugins))
	for _, p := range plugins {
		out = append(out, st.PluginDetail(p, withManifest))
	}
	s.writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) findPlugin(id string) (*core.State, *plugin.Plugin, error) {
	st, err := s.load()
	if err != nil {
		return nil, nil, err
	}
	p := core.FindPlugin(st.Evaluate(), id)
	if p == nil {
		return nil, nil, notFound("no such plugin")
	}
	return st, p, nil
}

func (s *Server) getPlugin(w http.ResponseWriter, r *http.Request) error {
	st, p, err := s.findPlugin(r.PathValue("id"))
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, st.PluginDetail(p, true))
	return nil
}

var pluginName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,31}$`)

var langPattern = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)

func (s *Server) getPluginMessages(w http.ResponseWriter, r *http.Request) error {
	lang := r.PathValue("lang")
	st, p, err := s.findPlugin(r.PathValue("id"))
	if err != nil {
		return err
	}
	if p.Manifest == nil || !langPattern.MatchString(lang) {
		return notFound("the plugin has no translations for " + lang)
	}
	msgs, err := st.Messages(p.Manifest, lang)
	if err != nil {
		return err
	}
	s.writeJSON(w, r, http.StatusOK, msgs)
	return nil
}
