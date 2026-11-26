package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

// Account is one Mashov parent login. Password and DevicePass are plaintext only
// in memory; they are sealed before they reach the database.
type Account struct {
	ID         int64
	Name       string
	Semel      int
	Year       int
	Username   string
	Password   string
	DevicePass string
	DeviceUUID string
	AuthState  string
	CreatedAt  time.Time
}

// CreateAccount inserts an account and returns it with its ID set.
func (s *Store) CreateAccount(ctx context.Context, a Account) (Account, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer func() { _ = tx.Rollback() }()

	if a.AuthState == "" {
		a.AuthState = "ok"
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO accounts(name, semel, year, username, device_uuid, auth_state, created_at) VALUES(?,?,?,?,?,?,?)`,
		a.Name, a.Semel, a.Year, a.Username, a.DeviceUUID, a.AuthState, a.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return a, fmt.Errorf("inserting account: %w", err)
	}
	a.ID, err = res.LastInsertId()
	if err != nil {
		return a, err
	}
	if err := s.setSecretTx(ctx, tx, a.ID, "password", a.Password); err != nil {
		return a, err
	}
	if err := s.setSecretTx(ctx, tx, a.ID, "devicepass", a.DevicePass); err != nil {
		return a, err
	}
	return a, tx.Commit()
}

// UpdateDevicePass stores (or clears, when empty) the sealed devicePass for an account.
func (s *Store) UpdateDevicePass(ctx context.Context, id int64, devicePass string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.setSecretTx(ctx, tx, id, "devicepass", devicePass); err != nil {
		return err
	}
	return tx.Commit()
}

// SetAuthState records the account's last authentication state.
func (s *Store) SetAuthState(ctx context.Context, id int64, state string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE accounts SET auth_state = ? WHERE id = ?`, state, id)
	return err
}

// GetAccount loads an account with its secrets unsealed.
func (s *Store) GetAccount(ctx context.Context, id int64) (Account, error) {
	var a Account
	var created string
	var pw, dp []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, semel, year, username, device_uuid, auth_state, created_at, password_sealed, devicepass_sealed
		 FROM accounts WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &a.Semel, &a.Year, &a.Username, &a.DeviceUUID, &a.AuthState, &created, &pw, &dp)
	if err != nil {
		return a, notFound(err)
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if a.Password, err = s.openSecret(pw, "password", a.ID); err != nil {
		return a, err
	}
	if a.DevicePass, err = s.openSecret(dp, "devicepass", a.ID); err != nil {
		return a, err
	}
	return a, nil
}

// ListAccounts returns all accounts without secrets.
func (s *Store) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, semel, year, username, device_uuid, auth_state, created_at FROM accounts ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		var created string
		if err := rows.Scan(&a.ID, &a.Name, &a.Semel, &a.Year, &a.Username, &a.DeviceUUID, &a.AuthState, &created); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) setSecretTx(ctx context.Context, tx *sql.Tx, id int64, field, value string) error {
	column := field + "_sealed" // "password_sealed" or "devicepass_sealed"
	var sealed []byte
	if value != "" {
		var err error
		sealed, err = s.sealer.Seal([]byte(value), secretAAD(field, id))
		if err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE accounts SET `+column+` = ? WHERE id = ?`, sealed, id)
	return err
}

func (s *Store) openSecret(sealed []byte, field string, id int64) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	pt, err := s.sealer.Open(sealed, secretAAD(field, id))
	if err != nil {
		return "", fmt.Errorf("opening %s for account %d: %w", field, id, err)
	}
	return string(pt), nil
}

// secretAAD binds a sealed value to its account and field, so it cannot be copied
// into another row or column.
func secretAAD(field string, id int64) []byte {
	return []byte("accounts." + field + ":" + strconv.FormatInt(id, 10))
}
