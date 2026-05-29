package db

import "fmt"

type CachedRoom struct {
	RoomID int    `json:"room_id"`
	Name   string `json:"name"`
}

func (d *DB) UpsertCachedRoom(roomID int, name string) error {
	_, err := d.conn.Exec(`
		INSERT INTO rooms_cache (room_id, name, updated_at)
		VALUES (?, ?, datetime('now'))
		ON CONFLICT(room_id) DO UPDATE SET
			name = excluded.name,
			updated_at = datetime('now')`,
		roomID, name,
	)
	if err != nil {
		return fmt.Errorf("upsert cached room: %w", err)
	}
	return nil
}

func (d *DB) GetCachedRoomName(roomID int) (string, error) {
	var name string
	err := d.conn.QueryRow(`SELECT name FROM rooms_cache WHERE room_id = ?`, roomID).Scan(&name)
	if err != nil {
		return "", fmt.Errorf("get cached room name: %w", err)
	}
	return name, nil
}
