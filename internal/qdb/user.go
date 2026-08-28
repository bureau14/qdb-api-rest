package qdb

import "log/slog"

// User is the QuasarDB user a pool of sessions authenticates as: the pool
// key is the user, never a REST session or a token (ADR-0003). A user has
// one secret key, so every REST session of a user dials identically and
// shares the user's pool. Anonymous is the zero User.
type User struct {
	Username  string
	SecretKey string
}

// LogValue renders a user without its secret key.
func (u User) LogValue() slog.Value {
	if u.Username == "" {
		return slog.StringValue("(anonymous)")
	}
	return slog.StringValue(u.Username)
}

// credentials identify one user to the cluster: username and secret key,
// or the user security file that carries both.
type credentials struct {
	username, secretKey, userSecurityFile string
}

func (u User) credentials() credentials {
	return credentials{username: u.Username, secretKey: u.SecretKey}
}
