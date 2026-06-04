// Package user holds a local read-cache of user identity, synced from
// user-auth-service over the `events.users` Redis channel. Mirrors the
// chat-service `user_snapshots` mechanism.
package user

import "time"

// Snapshot is a denormalized copy of a user's identity. The JSON tags match the
// `data` object published by user-auth-service (id/username/fullname/email/
// avatar/role/...), so an event payload unmarshals straight into it.
type Snapshot struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Fullname  string    `json:"fullname"`
	Username  string    `json:"username"`
	Avatar    *string   `json:"avatar"`
	Role      string    `json:"role"`
	UpdatedAt time.Time `json:"updated_at"`
}
