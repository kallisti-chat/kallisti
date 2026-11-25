package mumbled

import "errors"

// random image names are read atomically for every new image upload.
func (s *Server) RandomImageNames() bool {
	return s.randomImageNames.Load()
}

// save the image filename policy before publishing it (admins only).
func (s *Server) SetRandomImageNames(actor uint32, enabled bool) (bool, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	u := s.state.Users[actor]
	admin := u != nil && s.isAdmin(u)
	s.mu.Unlock()
	if !admin {
		return false, errors.New("only admins change this setting")
	}
	if s.cfg.DB != nil {
		if err := s.cfg.DB.setSetting("upload_random_image_names", enabled); err != nil {
			return false, err
		}
	}
	s.randomImageNames.Store(enabled)
	return enabled, nil
}
