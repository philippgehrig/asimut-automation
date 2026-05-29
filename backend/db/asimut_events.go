package db

import "fmt"

type AsimutEvent struct {
	EventID   int    `json:"event_id"`
	Title     string `json:"title"`
	RoomName  string `json:"room_name"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

func (d *DB) UpsertAsimutEvent(e AsimutEvent) error {
	_, err := d.conn.Exec(`
		INSERT INTO asimut_events (event_id, title, room_name, start_time, end_time, last_synced)
		VALUES (?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(event_id) DO UPDATE SET
			title = excluded.title,
			room_name = excluded.room_name,
			start_time = excluded.start_time,
			end_time = excluded.end_time,
			last_synced = datetime('now')`,
		e.EventID, e.Title, e.RoomName, e.StartTime, e.EndTime,
	)
	if err != nil {
		return fmt.Errorf("upsert asimut event: %w", err)
	}
	return nil
}

func (d *DB) ListAsimutEvents() ([]AsimutEvent, error) {
	rows, err := d.conn.Query(`
		SELECT event_id, title, room_name, start_time, end_time
		FROM asimut_events
		ORDER BY start_time`)
	if err != nil {
		return nil, fmt.Errorf("query asimut events: %w", err)
	}
	defer rows.Close()

	var events []AsimutEvent
	for rows.Next() {
		var e AsimutEvent
		if err := rows.Scan(&e.EventID, &e.Title, &e.RoomName, &e.StartTime, &e.EndTime); err != nil {
			return nil, fmt.Errorf("scan asimut event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (d *DB) DeleteAsimutEventsNotIn(eventIDs []int) error {
	if len(eventIDs) == 0 {
		_, err := d.conn.Exec(`DELETE FROM asimut_events`)
		return err
	}

	query := `DELETE FROM asimut_events WHERE event_id NOT IN (`
	args := make([]interface{}, len(eventIDs))
	for i, id := range eventIDs {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"

	_, err := d.conn.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("delete stale asimut events: %w", err)
	}
	return nil
}
