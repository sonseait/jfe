package server

import (
	"context"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/route"
	"jfe/backend/internal/store"
	"strings"
	"time"
)

func (s *Server) userDTO(ctx context.Context, u store.User) UserDTO {
	ids, err := s.DB.UserLibraries(ctx, u.ID)
	if err != nil || ids == nil {
		ids = []string{}
	}
	imports, _ := s.DB.UserImports(ctx, u.ID)
	return UserDTO{u.ID, u.Username, u.Role, u.Disabled, ids, imports}
}
func (s *Server) login(ctx context.Context, u store.User) (LoginDTO, error) {
	t := token()
	err := s.DB.InsertSession(ctx, store.InsertSessionParams{TokenHash: hash(t), UserID: u.ID, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)})
	return LoginDTO{t, s.userDTO(ctx, u)}, err
}
func (s *Server) setup(ctx context.Context, b SetupRequest) (LoginDTO, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return LoginDTO{}, err
	}
	defer tx.Rollback(ctx)
	q := s.DB.WithTx(tx)
	if err = q.LockSetup(ctx); err != nil {
		return LoginDTO{}, err
	}
	count, err := q.CountUsers(ctx)
	if err != nil {
		return LoginDTO{}, err
	}
	if count != 0 {
		return LoginDTO{}, route.Fail(409, "Setup already completed")
	}
	if len(b.Password) > 72 {
		return LoginDTO{}, route.Fail(422, "Password exceeds 72 bytes")
	}
	pw, err := bcrypt.GenerateFromPassword([]byte(b.Password), bcrypt.DefaultCost)
	if err != nil {
		return LoginDTO{}, err
	}
	u, err := q.CreateUser(ctx, store.CreateUserParams{ID: uuid.NewString(), Username: strings.TrimSpace(b.Username), PasswordHash: string(pw), Role: "admin"})
	if err != nil {
		return LoginDTO{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LoginDTO{}, err
	}
	return s.login(ctx, u)
}
func (s *Server) saveUser(ctx context.Context, id string, b UserRequest) (UserDTO, error) {
	p := route.User(ctx)
	if id == p.ID && (b.Disabled || b.Role != "admin") {
		return UserDTO{}, route.Fail(409, "Cannot disable or demote your own account")
	}
	if strings.TrimSpace(b.Username) == "" {
		return UserDTO{}, route.Fail(422, "Username required")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return UserDTO{}, err
	}
	defer tx.Rollback(ctx)
	q := s.DB.WithTx(tx)
	var u store.User
	if id == "" {
		if len(b.Password) < 8 {
			return UserDTO{}, route.Fail(422, "Password required")
		}
		pw, e := bcrypt.GenerateFromPassword([]byte(b.Password), bcrypt.DefaultCost)
		if e != nil {
			return UserDTO{}, e
		}
		u, err = q.CreateUser(ctx, store.CreateUserParams{ID: uuid.NewString(), Username: b.Username, PasswordHash: string(pw), Role: b.Role})
		if err == nil && b.Disabled {
			u, err = q.UpdateUser(ctx, store.UpdateUserParams{ID: u.ID, Username: u.Username, Role: u.Role, Disabled: true})
		}
	} else {
		u, err = q.UpdateUser(ctx, store.UpdateUserParams{ID: id, Username: b.Username, Role: b.Role, Disabled: b.Disabled})
		if err == nil && b.Password != "" {
			pw, e := bcrypt.GenerateFromPassword([]byte(b.Password), bcrypt.DefaultCost)
			if e != nil {
				return UserDTO{}, e
			}
			err = q.UpdatePassword(ctx, store.UpdatePasswordParams{ID: id, PasswordHash: string(pw)})
		}
		if err == nil {
			err = q.RevokeUserSessions(ctx, id)
		}
	}
	if err != nil {
		return UserDTO{}, err
	}
	imports := []string{}
	if b.ImportLibraryIDs == nil {
		imports, err = q.UserImports(ctx, u.ID)
		if err != nil {
			return UserDTO{}, err
		}
	} else {
		imports = *b.ImportLibraryIDs
	}
	if err = q.ClearAccess(ctx, u.ID); err != nil {
		return UserDTO{}, err
	}
	for _, lib := range b.LibraryIDs {
		if err = q.GrantLibrary(ctx, store.GrantLibraryParams{UserID: u.ID, LibraryID: lib}); err != nil {
			return UserDTO{}, err
		}
	}
	for _, lib := range imports {
		found := false
		for _, id := range b.LibraryIDs {
			if id == lib {
				found = true
			}
		}
		if !found && b.ImportLibraryIDs == nil {
			continue
		}
		if !found {
			return UserDTO{}, route.Fail(422, "Import permission requires library access")
		}
		library, e := q.GetLibrary(ctx, lib)
		if e != nil {
			return UserDTO{}, e
		}
		if !audio.IsLibrary(library.Kind) && library.Kind != "movies" && library.Kind != "series" {
			return UserDTO{}, route.Fail(422, "Import permission requires an audio or video library")
		}
		if err = q.GrantImport(ctx, store.GrantImportParams{UserID: u.ID, LibraryID: lib}); err != nil {
			return UserDTO{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return UserDTO{}, err
	}
	return s.userDTO(ctx, u), nil
}
