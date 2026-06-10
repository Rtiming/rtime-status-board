package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db                 *sql.DB
	path               string
	metricsRetention   time.Duration
	reportLogRetention time.Duration
	reportLogLimit     int
}

func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, path: path, metricsRetention: 30 * 24 * time.Hour, reportLogRetention: 7 * 24 * time.Hour, reportLogLimit: 2000}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SetMetricsRetention(retention time.Duration) {
	if retention > 0 {
		s.metricsRetention = retention
	}
}

func (s *Store) Diagnostics(ctx context.Context) (StoreDiagnostic, error) {
	diag := StoreDiagnostic{
		Path:                   s.path,
		MetricsRetentionDays:   s.metricsRetention.Hours() / 24,
		ReportLogRetentionDays: s.reportLogRetention.Hours() / 24,
		ReportLogLimit:         s.reportLogLimit,
	}

	var err error
	if diag.DBSizeBytes, err = fileSize(s.path); err != nil {
		return diag, err
	}
	if diag.WALSizeBytes, err = fileSize(s.path + "-wal"); err != nil {
		return diag, err
	}
	if diag.SHMSizeBytes, err = fileSize(s.path + "-shm"); err != nil {
		return diag, err
	}
	diag.TotalSizeBytes = diag.DBSizeBytes + diag.WALSizeBytes + diag.SHMSizeBytes

	if diag.StatusCacheRows, err = s.countRows(ctx, "status_cache"); err != nil {
		return diag, err
	}
	if diag.EventRows, err = s.countRows(ctx, "events"); err != nil {
		return diag, err
	}
	if diag.MetricsLatestRows, err = s.countRows(ctx, "metrics_latest"); err != nil {
		return diag, err
	}
	if diag.MetricsSampleRows, err = s.countRows(ctx, "metrics_samples"); err != nil {
		return diag, err
	}
	if diag.MetricsReportLogRows, err = s.countRows(ctx, "metrics_report_logs"); err != nil {
		return diag, err
	}
	if diag.LatestMetricAt, err = s.maxTime(ctx, "metrics_latest", "updated_at"); err != nil {
		return diag, err
	}
	if diag.LatestReportAt, err = s.maxTime(ctx, "metrics_report_logs", "received_at"); err != nil {
		return diag, err
	}

	return diag, nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func (s *Store) countRows(ctx context.Context, table string) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) maxTime(ctx context.Context, table, column string) (*time.Time, error) {
	var raw sql.NullString
	if err := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT MAX(%s) FROM %s", column, table)).Scan(&raw); err != nil {
		return nil, err
	}
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func (s *Store) init() error {
	statements := []string{
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS status_cache (
			kind TEXT NOT NULL,
			subject_id TEXT NOT NULL,
			label TEXT NOT NULL,
			status TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY (kind, subject_id)
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			kind TEXT NOT NULL,
			subject_id TEXT NOT NULL,
			label TEXT NOT NULL,
			from_status TEXT,
			to_status TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS metrics_latest (
			node_id TEXT PRIMARY KEY,
			hostname TEXT NOT NULL DEFAULT '',
			captured_at TEXT NOT NULL,
			cpu_percent REAL NOT NULL DEFAULT 0,
			load1 REAL NOT NULL DEFAULT 0,
			load5 REAL NOT NULL DEFAULT 0,
			load15 REAL NOT NULL DEFAULT 0,
			memory_total_bytes INTEGER NOT NULL DEFAULT 0,
			memory_used_bytes INTEGER NOT NULL DEFAULT 0,
			memory_percent REAL NOT NULL DEFAULT 0,
			swap_total_bytes INTEGER NOT NULL DEFAULT 0,
			swap_used_bytes INTEGER NOT NULL DEFAULT 0,
			swap_percent REAL NOT NULL DEFAULT 0,
			disk_mountpoint TEXT NOT NULL DEFAULT '/',
			disk_total_bytes INTEGER NOT NULL DEFAULT 0,
			disk_used_bytes INTEGER NOT NULL DEFAULT 0,
			disk_percent REAL NOT NULL DEFAULT 0,
			network_rx_bytes INTEGER NOT NULL DEFAULT 0,
			network_tx_bytes INTEGER NOT NULL DEFAULT 0,
			network_rx_bps REAL NOT NULL DEFAULT 0,
			network_tx_bps REAL NOT NULL DEFAULT 0,
			network_interfaces_json TEXT NOT NULL DEFAULT '[]',
			uptime_seconds REAL NOT NULL DEFAULT 0,
			extra_json TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS metrics_samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id TEXT NOT NULL,
			captured_at TEXT NOT NULL,
			schema_version INTEGER NOT NULL DEFAULT 1,
			cpu_percent REAL NOT NULL DEFAULT 0,
			memory_percent REAL NOT NULL DEFAULT 0,
			disk_percent REAL NOT NULL DEFAULT 0,
			network_rx_bytes INTEGER NOT NULL DEFAULT 0,
			network_tx_bytes INTEGER NOT NULL DEFAULT 0,
			network_rx_bps REAL NOT NULL DEFAULT 0,
			network_tx_bps REAL NOT NULL DEFAULT 0,
			storage_read_bytes INTEGER NOT NULL DEFAULT 0,
			storage_write_bytes INTEGER NOT NULL DEFAULT 0,
			storage_read_bps REAL NOT NULL DEFAULT 0,
			storage_write_bps REAL NOT NULL DEFAULT 0,
			storage_read_ios INTEGER NOT NULL DEFAULT 0,
			storage_write_ios INTEGER NOT NULL DEFAULT 0,
			storage_read_iops REAL NOT NULL DEFAULT 0,
			storage_write_iops REAL NOT NULL DEFAULT 0,
			gpu_available INTEGER NOT NULL DEFAULT 0,
			gpu_percent REAL NOT NULL DEFAULT 0,
			raw_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_samples_node_captured ON metrics_samples(node_id, captured_at)`,
		`CREATE TABLE IF NOT EXISTS metrics_report_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			node_id TEXT NOT NULL,
			hostname TEXT NOT NULL DEFAULT '',
			schema_version INTEGER NOT NULL DEFAULT 1,
			captured_at TEXT NOT NULL,
			received_at TEXT NOT NULL,
			report_lag_seconds REAL NOT NULL DEFAULT 0,
			collector_ok INTEGER NOT NULL DEFAULT 0,
			collector_failed INTEGER NOT NULL DEFAULT 0,
			collector_status_json TEXT NOT NULL DEFAULT '[]',
			gpu_available INTEGER NOT NULL DEFAULT 0,
			storage_device_count INTEGER NOT NULL DEFAULT 0,
			network_interface_count INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_report_logs_node_received ON metrics_report_logs(node_id, received_at)`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_report_logs_received ON metrics_report_logs(received_at)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return err
		}
	}
	if err := s.ensureMetricsLatestColumns(); err != nil {
		return err
	}
	return s.ensureMetricsSampleColumns()
}

type columnMigration struct {
	name       string
	definition string
}

func (s *Store) ensureMetricsLatestColumns() error {
	columns := []columnMigration{
		{name: "schema_version", definition: "INTEGER NOT NULL DEFAULT 1"},
		{name: "cpu_per_core_json", definition: "TEXT NOT NULL DEFAULT '[]'"},
		{name: "cpu_context_switches", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "cpu_interrupts", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "storage_read_bytes", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "storage_write_bytes", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "storage_read_bps", definition: "REAL NOT NULL DEFAULT 0"},
		{name: "storage_write_bps", definition: "REAL NOT NULL DEFAULT 0"},
		{name: "storage_read_iops", definition: "REAL NOT NULL DEFAULT 0"},
		{name: "storage_write_iops", definition: "REAL NOT NULL DEFAULT 0"},
		{name: "storage_devices_json", definition: "TEXT NOT NULL DEFAULT '[]'"},
		{name: "gpu_available", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "gpu_provider", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "gpu_devices_json", definition: "TEXT NOT NULL DEFAULT '[]'"},
		{name: "containers_json", definition: "TEXT NOT NULL DEFAULT '{}'"},
		{name: "processes_json", definition: "TEXT NOT NULL DEFAULT '{}'"},
		{name: "collector_status_json", definition: "TEXT NOT NULL DEFAULT '[]'"},
	}
	existing := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA table_info(metrics_latest)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, column := range columns {
		if existing[column.name] {
			continue
		}
		if _, err := s.db.Exec(fmt.Sprintf("ALTER TABLE metrics_latest ADD COLUMN %s %s", column.name, column.definition)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureMetricsSampleColumns() error {
	columns := []columnMigration{
		{name: "storage_read_ios", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "storage_write_ios", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "storage_read_iops", definition: "REAL NOT NULL DEFAULT 0"},
		{name: "storage_write_iops", definition: "REAL NOT NULL DEFAULT 0"},
	}
	existing := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA table_info(metrics_samples)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, column := range columns {
		if existing[column.name] {
			continue
		}
		if _, err := s.db.Exec(fmt.Sprintf("ALTER TABLE metrics_samples ADD COLUMN %s %s", column.name, column.definition)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveMetrics(ctx context.Context, report MetricsReport) (MetricsView, error) {
	now := time.Now().UTC()
	if report.CapturedAt.IsZero() {
		report.CapturedAt = now
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MetricsView{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var prevRX, prevTX int64
	var prevCaptured string
	selectErr := tx.QueryRowContext(ctx, `SELECT network_rx_bytes, network_tx_bytes, captured_at FROM metrics_latest WHERE node_id = ?`, report.NodeID).Scan(&prevRX, &prevTX, &prevCaptured)
	if selectErr != nil && !errors.Is(selectErr, sql.ErrNoRows) {
		err = selectErr
		return MetricsView{}, err
	}
	if selectErr == nil {
		if previousTime, parseErr := time.Parse(time.RFC3339Nano, prevCaptured); parseErr == nil {
			seconds := report.CapturedAt.Sub(previousTime).Seconds()
			if seconds > 0 {
				if report.Network.RXBytes >= prevRX {
					report.Network.RXBps = float64(report.Network.RXBytes-prevRX) / seconds
				}
				if report.Network.TXBytes >= prevTX {
					report.Network.TXBps = float64(report.Network.TXBytes-prevTX) / seconds
				}
			}
		}
	}

	interfacesJSON, err := json.Marshal(report.Network.Interfaces)
	if err != nil {
		return MetricsView{}, err
	}
	extraJSON, err := json.Marshal(report.Extra)
	if err != nil {
		return MetricsView{}, err
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO metrics_latest(
		node_id, hostname, captured_at, cpu_percent, load1, load5, load15,
		memory_total_bytes, memory_used_bytes, memory_percent,
		swap_total_bytes, swap_used_bytes, swap_percent,
		disk_mountpoint, disk_total_bytes, disk_used_bytes, disk_percent,
		network_rx_bytes, network_tx_bytes, network_rx_bps, network_tx_bps,
		network_interfaces_json, uptime_seconds, extra_json, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(node_id) DO UPDATE SET
		hostname = excluded.hostname,
		captured_at = excluded.captured_at,
		cpu_percent = excluded.cpu_percent,
		load1 = excluded.load1,
		load5 = excluded.load5,
		load15 = excluded.load15,
		memory_total_bytes = excluded.memory_total_bytes,
		memory_used_bytes = excluded.memory_used_bytes,
		memory_percent = excluded.memory_percent,
		swap_total_bytes = excluded.swap_total_bytes,
		swap_used_bytes = excluded.swap_used_bytes,
		swap_percent = excluded.swap_percent,
		disk_mountpoint = excluded.disk_mountpoint,
		disk_total_bytes = excluded.disk_total_bytes,
		disk_used_bytes = excluded.disk_used_bytes,
		disk_percent = excluded.disk_percent,
		network_rx_bytes = excluded.network_rx_bytes,
		network_tx_bytes = excluded.network_tx_bytes,
		network_rx_bps = excluded.network_rx_bps,
		network_tx_bps = excluded.network_tx_bps,
		network_interfaces_json = excluded.network_interfaces_json,
		uptime_seconds = excluded.uptime_seconds,
		extra_json = excluded.extra_json,
		updated_at = excluded.updated_at`,
		report.NodeID, report.Hostname, report.CapturedAt.Format(time.RFC3339Nano), report.CPU.Percent, report.CPU.Load1, report.CPU.Load5, report.CPU.Load15,
		report.Memory.TotalBytes, report.Memory.UsedBytes, report.Memory.Percent,
		report.Swap.TotalBytes, report.Swap.UsedBytes, report.Swap.Percent,
		report.Disk.Mountpoint, report.Disk.TotalBytes, report.Disk.UsedBytes, report.Disk.Percent,
		report.Network.RXBytes, report.Network.TXBytes, report.Network.RXBps, report.Network.TXBps,
		string(interfacesJSON), report.Uptime.Seconds, string(extraJSON), now.Format(time.RFC3339Nano))
	if err != nil {
		return MetricsView{}, err
	}
	err = tx.Commit()
	if err != nil {
		return MetricsView{}, err
	}

	return MetricsView{MetricsReport: report, SchemaVersion: 1, UpdatedAt: now, Stale: now.Sub(report.CapturedAt) > 2*time.Minute}, nil
}

func (s *Store) SaveMetricsV2(ctx context.Context, report MetricsReportV2) (MetricsView, MetricsHistoryPoint, error) {
	if report.SchemaVersion == 0 {
		report.SchemaVersion = 2
	}
	if report.CapturedAt.IsZero() {
		report.CapturedAt = time.Now().UTC()
	}

	storageReadIOs, storageWriteIOs := storageIOTotals(report.Resources.Storage)
	storageReadBps, storageWriteBps, storageReadIOPS, storageWriteIOPS, err := s.storageRates(ctx, report.NodeID, report.CapturedAt, report.Resources.Storage.ReadBytes, report.Resources.Storage.WriteBytes, storageReadIOs, storageWriteIOs)
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}
	report.Resources.Storage.ReadBps = storageReadBps
	report.Resources.Storage.WriteBps = storageWriteBps
	report.Resources.Storage.ReadIOPS = storageReadIOPS
	report.Resources.Storage.WriteIOPS = storageWriteIOPS

	view, err := s.SaveMetrics(ctx, MetricsReport{
		NodeID:     report.NodeID,
		Hostname:   report.Hostname,
		CapturedAt: report.CapturedAt,
		CPU:        report.Resources.CPU.CPUMetrics,
		Memory:     report.Resources.Memory,
		Swap:       report.Resources.Swap,
		Disk:       report.Resources.Disk,
		Network:    networkV2ToV1(report.Resources.Network),
		Uptime:     report.Resources.Uptime,
		Extra:      report.Extra,
	})
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}

	point := MetricsHistoryPoint{
		NodeID:           report.NodeID,
		CapturedAt:       report.CapturedAt,
		CPUPercent:       report.Resources.CPU.Percent,
		MemoryPercent:    report.Resources.Memory.Percent,
		DiskPercent:      report.Resources.Disk.Percent,
		NetworkRXBps:     view.Network.RXBps,
		NetworkTXBps:     view.Network.TXBps,
		StorageReadBps:   storageReadBps,
		StorageWriteBps:  storageWriteBps,
		StorageReadIOPS:  storageReadIOPS,
		StorageWriteIOPS: storageWriteIOPS,
		GPUAvailable:     report.Resources.GPU.Available,
		GPUPercent:       firstGPUPercent(report.Resources.GPU),
	}

	rawJSON, err := json.Marshal(report)
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}
	cpuPerCoreJSON, err := json.Marshal(report.Resources.CPU.PerCorePercent)
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}
	storageDevicesJSON, err := json.Marshal(report.Resources.Storage.Devices)
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}
	gpuDevicesJSON, err := json.Marshal(report.Resources.GPU.Devices)
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}
	containersJSON, err := json.Marshal(report.Resources.Containers)
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}
	processesJSON, err := json.Marshal(report.Resources.Processes)
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}
	collectorStatusJSON, err := json.Marshal(report.CollectorStatus)
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}
	collectorOK, collectorFailed := collectorCounts(report.CollectorStatus)
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.ExecContext(ctx, `UPDATE metrics_latest SET
		schema_version = ?,
		cpu_per_core_json = ?,
		cpu_context_switches = ?,
		cpu_interrupts = ?,
		storage_read_bytes = ?,
		storage_write_bytes = ?,
		storage_read_bps = ?,
		storage_write_bps = ?,
		storage_read_iops = ?,
		storage_write_iops = ?,
		storage_devices_json = ?,
		gpu_available = ?,
		gpu_provider = ?,
		gpu_devices_json = ?,
		containers_json = ?,
		processes_json = ?,
		collector_status_json = ?,
		updated_at = ?
		WHERE node_id = ?`,
		report.SchemaVersion,
		string(cpuPerCoreJSON),
		report.Resources.CPU.ContextSwitches,
		report.Resources.CPU.Interrupts,
		report.Resources.Storage.ReadBytes,
		report.Resources.Storage.WriteBytes,
		point.StorageReadBps,
		point.StorageWriteBps,
		point.StorageReadIOPS,
		point.StorageWriteIOPS,
		string(storageDevicesJSON),
		boolToInt(report.Resources.GPU.Available),
		report.Resources.GPU.Provider,
		string(gpuDevicesJSON),
		string(containersJSON),
		string(processesJSON),
		string(collectorStatusJSON),
		now.Format(time.RFC3339Nano),
		report.NodeID)
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO metrics_samples(
		node_id, captured_at, schema_version, cpu_percent, memory_percent, disk_percent,
		network_rx_bytes, network_tx_bytes, network_rx_bps, network_tx_bps,
		storage_read_bytes, storage_write_bytes, storage_read_bps, storage_write_bps,
		storage_read_ios, storage_write_ios, storage_read_iops, storage_write_iops,
		gpu_available, gpu_percent, raw_json, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		report.NodeID, report.CapturedAt.Format(time.RFC3339Nano), report.SchemaVersion,
		point.CPUPercent, point.MemoryPercent, point.DiskPercent,
		report.Resources.Network.RXBytes, report.Resources.Network.TXBytes, point.NetworkRXBps, point.NetworkTXBps,
		report.Resources.Storage.ReadBytes, report.Resources.Storage.WriteBytes, point.StorageReadBps, point.StorageWriteBps,
		storageReadIOs, storageWriteIOs, point.StorageReadIOPS, point.StorageWriteIOPS,
		boolToInt(point.GPUAvailable), point.GPUPercent, string(rawJSON), now.Format(time.RFC3339Nano))
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO metrics_report_logs(
		node_id, hostname, schema_version, captured_at, received_at, report_lag_seconds,
		collector_ok, collector_failed, collector_status_json, gpu_available,
		storage_device_count, network_interface_count
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		report.NodeID, report.Hostname, report.SchemaVersion,
		report.CapturedAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Sub(report.CapturedAt).Seconds(),
		collectorOK, collectorFailed, string(collectorStatusJSON), boolToInt(report.Resources.GPU.Available),
		len(report.Resources.Storage.Devices), len(report.Resources.Network.Interfaces))
	if err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}
	if s.metricsRetention > 0 {
		_, err = tx.ExecContext(ctx, `DELETE FROM metrics_samples WHERE captured_at < ?`, report.CapturedAt.Add(-s.metricsRetention).Format(time.RFC3339Nano))
		if err != nil {
			return MetricsView{}, MetricsHistoryPoint{}, err
		}
	}
	if s.reportLogRetention > 0 {
		_, err = tx.ExecContext(ctx, `DELETE FROM metrics_report_logs WHERE received_at < ?`, now.Add(-s.reportLogRetention).Format(time.RFC3339Nano))
		if err != nil {
			return MetricsView{}, MetricsHistoryPoint{}, err
		}
	}
	if s.reportLogLimit > 0 {
		_, err = tx.ExecContext(ctx, `DELETE FROM metrics_report_logs
			WHERE id NOT IN (
				SELECT id FROM metrics_report_logs ORDER BY id DESC LIMIT ?
			)`, s.reportLogLimit)
		if err != nil {
			return MetricsView{}, MetricsHistoryPoint{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return MetricsView{}, MetricsHistoryPoint{}, err
	}
	view.SchemaVersion = report.SchemaVersion
	view.CPU = report.Resources.CPU.CPUMetrics
	view.Storage = report.Resources.Storage
	view.GPU = report.Resources.GPU
	view.Containers = report.Resources.Containers
	view.Processes = report.Resources.Processes
	view.CollectorStatus = report.CollectorStatus
	return view, point, nil
}

func (s *Store) storageRates(ctx context.Context, nodeID string, capturedAt time.Time, readBytes, writeBytes, readIOs, writeIOs int64) (float64, float64, float64, float64, error) {
	var prevRead, prevWrite, prevReadIOs, prevWriteIOs int64
	var prevCaptured, rawJSON string
	err := s.db.QueryRowContext(ctx, `SELECT storage_read_bytes, storage_write_bytes, storage_read_ios, storage_write_ios, captured_at, raw_json
		FROM metrics_samples WHERE node_id = ? ORDER BY captured_at DESC LIMIT 1`, nodeID).Scan(&prevRead, &prevWrite, &prevReadIOs, &prevWriteIOs, &prevCaptured, &rawJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0, 0, nil
	}
	if err != nil {
		return 0, 0, 0, 0, err
	}
	hasPrevReadIOs := prevReadIOs > 0
	hasPrevWriteIOs := prevWriteIOs > 0
	if (!hasPrevReadIOs || !hasPrevWriteIOs) && rawJSON != "" {
		rawReadIOs, rawWriteIOs := storageIOTotalsFromRaw(rawJSON)
		if !hasPrevReadIOs && rawReadIOs > 0 {
			prevReadIOs = rawReadIOs
			hasPrevReadIOs = true
		}
		if !hasPrevWriteIOs && rawWriteIOs > 0 {
			prevWriteIOs = rawWriteIOs
			hasPrevWriteIOs = true
		}
	}
	previousTime, err := time.Parse(time.RFC3339Nano, prevCaptured)
	if err != nil {
		return 0, 0, 0, 0, nil
	}
	seconds := capturedAt.Sub(previousTime).Seconds()
	if seconds <= 0 {
		return 0, 0, 0, 0, nil
	}
	var readBps, writeBps, readIOPS, writeIOPS float64
	if readBytes >= prevRead {
		readBps = float64(readBytes-prevRead) / seconds
	}
	if writeBytes >= prevWrite {
		writeBps = float64(writeBytes-prevWrite) / seconds
	}
	if hasPrevReadIOs && readIOs >= prevReadIOs {
		readIOPS = float64(readIOs-prevReadIOs) / seconds
	}
	if hasPrevWriteIOs && writeIOs >= prevWriteIOs {
		writeIOPS = float64(writeIOs-prevWriteIOs) / seconds
	}
	return readBps, writeBps, readIOPS, writeIOPS, nil
}

func storageIOTotals(storage StorageMetrics) (int64, int64) {
	var readIOs, writeIOs int64
	for _, device := range storage.Devices {
		readIOs += device.ReadIOs
		writeIOs += device.WriteIOs
	}
	return readIOs, writeIOs
}

func storageIOTotalsFromRaw(rawJSON string) (int64, int64) {
	var report MetricsReportV2
	if err := json.Unmarshal([]byte(rawJSON), &report); err != nil {
		return 0, 0
	}
	return storageIOTotals(report.Resources.Storage)
}

func (s *Store) MetricsHistory(ctx context.Context, nodeID string, since time.Time, limit int) ([]MetricsHistoryPoint, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT node_id, captured_at, cpu_percent, memory_percent, disk_percent,
		network_rx_bps, network_tx_bps, storage_read_bps, storage_write_bps, storage_read_iops, storage_write_iops,
		gpu_available, gpu_percent
		FROM (
			SELECT node_id, captured_at, cpu_percent, memory_percent, disk_percent,
				network_rx_bps, network_tx_bps, storage_read_bps, storage_write_bps, storage_read_iops, storage_write_iops,
				gpu_available, gpu_percent
			FROM metrics_samples
			WHERE node_id = ? AND captured_at >= ?
			ORDER BY captured_at DESC
			LIMIT ?
		)
		ORDER BY captured_at ASC`, nodeID, since.Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := []MetricsHistoryPoint{}
	for rows.Next() {
		var point MetricsHistoryPoint
		var capturedAt string
		var gpuAvailable int
		err := rows.Scan(&point.NodeID, &capturedAt, &point.CPUPercent, &point.MemoryPercent, &point.DiskPercent,
			&point.NetworkRXBps, &point.NetworkTXBps, &point.StorageReadBps, &point.StorageWriteBps,
			&point.StorageReadIOPS, &point.StorageWriteIOPS, &gpuAvailable, &point.GPUPercent)
		if err != nil {
			return nil, err
		}
		point.CapturedAt, err = time.Parse(time.RFC3339Nano, capturedAt)
		if err != nil {
			return nil, err
		}
		point.GPUAvailable = gpuAvailable == 1
		points = append(points, point)
	}
	return points, rows.Err()
}

func networkV2ToV1(network NetworkMetricsV2) NetworkMetrics {
	interfaces := make([]NetworkInterfaceMetric, 0, len(network.Interfaces))
	for _, iface := range network.Interfaces {
		interfaces = append(interfaces, iface)
	}
	return NetworkMetrics{
		RXBytes:    network.RXBytes,
		TXBytes:    network.TXBytes,
		RXBps:      network.RXBps,
		TXBps:      network.TXBps,
		Interfaces: interfaces,
	}
}

func firstGPUPercent(gpu GPUMetrics) float64 {
	if !gpu.Available || len(gpu.Devices) == 0 {
		return 0
	}
	return gpu.Devices[0].UtilPercent
}

func collectorCounts(statuses []CollectorStatus) (int, int) {
	okCount := 0
	failedCount := 0
	for _, status := range statuses {
		if status.OK {
			okCount++
		} else {
			failedCount++
		}
	}
	return okCount, failedCount
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Store) RecentMetricReports(ctx context.Context, nodeID string, limit int) ([]MetricsReportLog, error) {
	if limit <= 0 {
		limit = 30
	}
	if limit > 200 {
		limit = 200
	}
	query := `SELECT id, node_id, hostname, schema_version, captured_at, received_at, report_lag_seconds,
		collector_ok, collector_failed, collector_status_json, gpu_available, storage_device_count, network_interface_count
		FROM metrics_report_logs`
	args := []any{}
	if nodeID != "" {
		query += ` WHERE node_id = ?`
		args = append(args, nodeID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []MetricsReportLog{}
	for rows.Next() {
		var log MetricsReportLog
		var capturedAt, receivedAt, collectorStatusJSON string
		var gpuAvailable int
		err := rows.Scan(&log.ID, &log.NodeID, &log.Hostname, &log.SchemaVersion, &capturedAt, &receivedAt, &log.ReportLagSeconds,
			&log.CollectorOK, &log.CollectorFailed, &collectorStatusJSON, &gpuAvailable, &log.StorageDeviceCount, &log.NetworkInterfaceCount)
		if err != nil {
			return nil, err
		}
		log.CapturedAt, err = time.Parse(time.RFC3339Nano, capturedAt)
		if err != nil {
			return nil, err
		}
		log.ReceivedAt, err = time.Parse(time.RFC3339Nano, receivedAt)
		if err != nil {
			return nil, err
		}
		log.GPUAvailable = gpuAvailable == 1
		_ = json.Unmarshal([]byte(collectorStatusJSON), &log.CollectorStatus)
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func (s *Store) LatestMetrics(ctx context.Context) ([]MetricsView, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id, hostname, captured_at, cpu_percent, load1, load5, load15,
		memory_total_bytes, memory_used_bytes, memory_percent,
		swap_total_bytes, swap_used_bytes, swap_percent,
		disk_mountpoint, disk_total_bytes, disk_used_bytes, disk_percent,
		network_rx_bytes, network_tx_bytes, network_rx_bps, network_tx_bps,
		network_interfaces_json, uptime_seconds, extra_json, updated_at,
		schema_version, cpu_per_core_json, cpu_context_switches, cpu_interrupts,
		storage_read_bytes, storage_write_bytes, storage_read_bps, storage_write_bps, storage_read_iops, storage_write_iops,
		storage_devices_json, gpu_available, gpu_provider, gpu_devices_json,
		containers_json, processes_json, collector_status_json
		FROM metrics_latest ORDER BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	metrics := []MetricsView{}
	now := time.Now().UTC()
	for rows.Next() {
		var view MetricsView
		var capturedAt, updatedAt string
		var interfacesJSON, extraJSON string
		var cpuPerCoreJSON, storageDevicesJSON, gpuDevicesJSON, containersJSON, processesJSON, collectorStatusJSON string
		var gpuAvailable int
		err := rows.Scan(&view.NodeID, &view.Hostname, &capturedAt, &view.CPU.Percent, &view.CPU.Load1, &view.CPU.Load5, &view.CPU.Load15,
			&view.Memory.TotalBytes, &view.Memory.UsedBytes, &view.Memory.Percent,
			&view.Swap.TotalBytes, &view.Swap.UsedBytes, &view.Swap.Percent,
			&view.Disk.Mountpoint, &view.Disk.TotalBytes, &view.Disk.UsedBytes, &view.Disk.Percent,
			&view.Network.RXBytes, &view.Network.TXBytes, &view.Network.RXBps, &view.Network.TXBps,
			&interfacesJSON, &view.Uptime.Seconds, &extraJSON, &updatedAt,
			&view.SchemaVersion, &cpuPerCoreJSON, &view.CPU.ContextSwitches, &view.CPU.Interrupts,
			&view.Storage.ReadBytes, &view.Storage.WriteBytes, &view.Storage.ReadBps, &view.Storage.WriteBps,
			&view.Storage.ReadIOPS, &view.Storage.WriteIOPS,
			&storageDevicesJSON, &gpuAvailable, &view.GPU.Provider, &gpuDevicesJSON,
			&containersJSON, &processesJSON, &collectorStatusJSON)
		if err != nil {
			return nil, err
		}
		view.CapturedAt, err = time.Parse(time.RFC3339Nano, capturedAt)
		if err != nil {
			return nil, err
		}
		view.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(interfacesJSON), &view.Network.Interfaces)
		_ = json.Unmarshal([]byte(extraJSON), &view.Extra)
		_ = json.Unmarshal([]byte(cpuPerCoreJSON), &view.CPU.PerCorePercent)
		_ = json.Unmarshal([]byte(storageDevicesJSON), &view.Storage.Devices)
		_ = json.Unmarshal([]byte(gpuDevicesJSON), &view.GPU.Devices)
		_ = json.Unmarshal([]byte(containersJSON), &view.Containers)
		_ = json.Unmarshal([]byte(processesJSON), &view.Processes)
		_ = json.Unmarshal([]byte(collectorStatusJSON), &view.CollectorStatus)
		view.GPU.Available = gpuAvailable == 1
		view.Stale = now.Sub(view.UpdatedAt) > 2*time.Minute
		metrics = append(metrics, view)
	}
	return metrics, rows.Err()
}

func (s *Store) RecordStatus(ctx context.Context, kind, subjectID, label string, status Status, detail string) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var previous string
	selectErr := tx.QueryRowContext(ctx, `SELECT status FROM status_cache WHERE kind = ? AND subject_id = ?`, kind, subjectID).Scan(&previous)
	if selectErr != nil && !errors.Is(selectErr, sql.ErrNoRows) {
		err = selectErr
		return err
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO status_cache(kind, subject_id, label, status, detail, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(kind, subject_id) DO UPDATE SET
			label = excluded.label,
			status = excluded.status,
			detail = excluded.detail,
			updated_at = excluded.updated_at`,
		kind, subjectID, label, string(status), detail, now.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}

	if selectErr == nil && previous != string(status) {
		_, err = tx.ExecContext(ctx, `INSERT INTO events(kind, subject_id, label, from_status, to_status, detail, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			kind, subjectID, label, previous, string(status), detail, now.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}

	err = tx.Commit()
	return err
}

func (s *Store) RecentEvents(ctx context.Context, limit int) ([]Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, subject_id, label, COALESCE(from_status, ''), to_status, detail, created_at
		FROM events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var event Event
		var from, to, created string
		if err := rows.Scan(&event.ID, &event.Kind, &event.SubjectID, &event.Label, &from, &to, &event.Detail, &created); err != nil {
			return nil, err
		}
		event.From = Status(from)
		event.To = Status(to)
		event.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("parse event time: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
