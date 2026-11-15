package server

import "github.com/Autumn-27/artex/config"

// trMsg translates an English source message into the active output language.
// Legacy Korean messages are accepted for compatibility with persisted values.
// Unknown messages pass through unchanged. Translate format templates before
// passing them to fmt.Sprintf or fmt.Errorf so catalog lookup uses the exact key.
// config.ActiveLanguage reflects the current runtime setting immediately.
func trMsg(message string) string {
	translations, ok := errCatalog[message]
	if !ok {
		translations, ok = legacyErrCatalog[message]
	}
	if ok {
		if translated := translations[config.ActiveLanguage()]; translated != "" {
			return translated
		}
	}
	return message
}

// loadOutputLanguage resolves the user-facing output language from the source of
// truth: the runtime setting (settings table) over config.Language() (ARTEX_LANG >
// config file > en). Call it only to (re)fill the cache; it does a DB query and a
// config file read.
func (s *Server) loadOutputLanguage() string {
	if s.m != nil && s.m.pg != nil {
		if v, ok, _ := s.m.pg.GetSetting(settingOutputLanguage); ok {
			if nv := config.NormalizeLanguage(v); nv != "" {
				return nv
			}
		}
	}
	return config.Language()
}

// outputLanguage returns the cached output language, resolving it on first use.
func (s *Server) outputLanguage() string {
	if v, ok := s.outputLang.Load().(string); ok && v != "" {
		return v
	}
	v := s.loadOutputLanguage()
	s.outputLang.Store(v)
	return v
}
