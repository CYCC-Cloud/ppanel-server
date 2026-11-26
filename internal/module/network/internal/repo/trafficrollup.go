package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/perfect-panel/backend/internal/module/network/entity/traffic"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/orm"
	"github.com/perfect-panel/backend/pkg/timeutil"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ repository.TrafficRollupRepo = (*trafficRollupRepo)(nil)

// trafficRollupBatchSize bounds one insert of a bucket's rows.
const trafficRollupBatchSize = 1000

// trafficRollupRepo owns the fork's traffic rollup tables. Every query names
// its columns; the tables are created by fork migration 00004, never by
// AutoMigrate.
type trafficRollupRepo struct {
	Conn *gorm.DB
}

// NewTrafficRollupRepo builds the module-owned implementation.
func NewTrafficRollupRepo(db *gorm.DB) repository.TrafficRollupRepo {
	return &trafficRollupRepo{Conn: db}
}

func (m *trafficRollupRepo) OldestTrafficLog(ctx context.Context) (time.Time, bool, error) {
	return m.bucketTimestamp(ctx, &traffic.TrafficLog{}, "timestamp", "ASC")
}

func (m *trafficRollupRepo) OldestHourlyStart(ctx context.Context) (time.Time, bool, error) {
	return m.bucketTimestamp(ctx, &traffic.TrafficHourly{}, "hour_start", "ASC")
}

func (m *trafficRollupRepo) LatestHourlyStart(ctx context.Context) (time.Time, bool, error) {
	return m.bucketTimestamp(ctx, &traffic.TrafficHourly{}, "hour_start", "DESC")
}

// bucketTimestamp returns the first or last timestamp of the table, and false
// when it holds none. Reading the column in order keeps its type, which an
// aggregate would lose on SQLite.
func (m *trafficRollupRepo) bucketTimestamp(ctx context.Context, model any, column, direction string) (time.Time, bool, error) {
	var row struct{ Value *time.Time }
	err := m.Conn.WithContext(ctx).Model(model).
		Select(rollupColumn(m.Conn, column) + " AS value").
		Order(rollupColumn(m.Conn, column) + " " + direction).
		Limit(1).
		Scan(&row).Error
	if err != nil || row.Value == nil {
		return time.Time{}, false, err
	}
	return *row.Value, true, nil
}

func (m *trafficRollupRepo) TrafficLogSums(ctx context.Context, start, end time.Time) ([]traffic.TrafficSum, error) {
	var rows []traffic.TrafficSum
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).
		Select(rollupColumn(m.Conn, "subscribe_id")+" AS "+rollupColumn(m.Conn, "user_subscribe_id")+", "+
			rollupColumn(m.Conn, "user_id")+", "+rollupColumn(m.Conn, "server_id")+", "+
			m.sumExpr("upload")+", "+m.sumExpr("download")).
		Where(rollupColumn(m.Conn, "timestamp")+" >= ? AND "+rollupColumn(m.Conn, "timestamp")+" < ?", start, end).
		Group("subscribe_id, user_id, server_id").
		Scan(&rows).Error
	return rows, err
}

func (m *trafficRollupRepo) ReplaceHourly(ctx context.Context, hourStart time.Time, rows []traffic.TrafficSum) error {
	return m.Conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(rollupColumn(tx, "hour_start")+" = ?", hourStart).Delete(&traffic.TrafficHourly{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		now := timeutil.Now()
		models := make([]*traffic.TrafficHourly, 0, len(rows))
		for _, row := range rows {
			models = append(models, &traffic.TrafficHourly{
				HourStart:       hourStart,
				UserSubscribeId: row.UserSubscribeId,
				UserId:          row.UserId,
				ServerId:        row.ServerId,
				Upload:          row.Upload,
				Download:        row.Download,
				UpdatedAt:       now,
			})
		}
		return tx.CreateInBatches(models, trafficRollupBatchSize).Error
	})
}

func (m *trafficRollupRepo) HourlySums(ctx context.Context, start, end time.Time) ([]traffic.TrafficSum, error) {
	var rows []traffic.TrafficSum
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficHourly{}).
		Select(rollupColumn(m.Conn, "user_subscribe_id")+", "+rollupColumn(m.Conn, "user_id")+", "+
			rollupColumn(m.Conn, "server_id")+", "+m.sumExpr("upload")+", "+m.sumExpr("download")).
		Where(rollupColumn(m.Conn, "hour_start")+" >= ? AND "+rollupColumn(m.Conn, "hour_start")+" < ?", start, end).
		Group("user_subscribe_id, user_id, server_id").
		Scan(&rows).Error
	return rows, err
}

func (m *trafficRollupRepo) ReplaceDaily(ctx context.Context, day string, rows []traffic.TrafficSum) error {
	return m.Conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(rollupColumn(tx, "day")+" = ?", day).Delete(&traffic.TrafficDaily{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		now := timeutil.Now()
		models := make([]*traffic.TrafficDaily, 0, len(rows))
		for _, row := range rows {
			models = append(models, &traffic.TrafficDaily{
				Day:             day,
				UserSubscribeId: row.UserSubscribeId,
				UserId:          row.UserId,
				ServerId:        row.ServerId,
				Upload:          row.Upload,
				Download:        row.Download,
				UpdatedAt:       now,
			})
		}
		return tx.CreateInBatches(models, trafficRollupBatchSize).Error
	})
}

func (m *trafficRollupRepo) DeleteBeforeBatch(ctx context.Context, before time.Time, limit int) (int64, error) {
	return m.deleteBeforeBatch(ctx, &traffic.TrafficHourly{}, rollupColumn(m.Conn, "hour_start"), before, limit)
}

func (m *trafficRollupRepo) DeleteDailyBeforeBatch(ctx context.Context, before string, limit int) (int64, error) {
	return m.deleteBeforeBatch(ctx, &traffic.TrafficDaily{}, rollupColumn(m.Conn, "day"), before, limit)
}

// deleteBeforeBatch deletes up to limit rows older than before in two steps:
// neither MySQL nor PostgreSQL accepts LIMIT on a DELETE.
func (m *trafficRollupRepo) deleteBeforeBatch(ctx context.Context, model any, column string, before any, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	var ids []int64
	if err := m.Conn.WithContext(ctx).Model(model).
		Select("id").Where(column+" < ?", before).
		Order("id ASC").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := m.Conn.WithContext(ctx).Where("id IN ?", ids).Delete(model)
	return result.RowsAffected, result.Error
}

func (m *trafficRollupRepo) TrafficLogPoints(ctx context.Context, userSubscribeID, userID int64, start, end time.Time) ([]traffic.TrafficPoint, error) {
	var rows []traffic.TrafficPoint
	timestamp := rollupColumn(m.Conn, "timestamp")
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficLog{}).
		Select(timestamp+" AS "+rollupColumn(m.Conn, "start")+", "+
			rollupColumn(m.Conn, "upload")+", "+rollupColumn(m.Conn, "download")).
		Where("subscribe_id = ? AND user_id = ? AND "+timestamp+" >= ? AND "+timestamp+" < ?",
			userSubscribeID, userID, start, end).
		Order(timestamp + " ASC").
		Scan(&rows).Error
	return rows, err
}

func (m *trafficRollupRepo) HourPoints(ctx context.Context, userSubscribeID, userID int64, start, end time.Time) ([]traffic.TrafficPoint, error) {
	var rows []traffic.TrafficPoint
	hourStart := rollupColumn(m.Conn, "hour_start")
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficHourly{}).
		Select(hourStart+" AS "+rollupColumn(m.Conn, "start")+", "+m.sumExpr("upload")+", "+m.sumExpr("download")).
		Where("user_subscribe_id = ? AND user_id = ? AND "+hourStart+" >= ? AND "+hourStart+" < ?",
			userSubscribeID, userID, start, end).
		Group(hourStart).
		Order(hourStart + " ASC").
		Scan(&rows).Error
	return rows, err
}

func (m *trafficRollupRepo) DayPoints(ctx context.Context, userSubscribeID, userID int64, fromDay, toDay string) ([]traffic.DayPoint, error) {
	var rows []traffic.DayPoint
	day := rollupColumn(m.Conn, "day")
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficDaily{}).
		Select(day+", "+m.sumExpr("upload")+", "+m.sumExpr("download")).
		Where("user_subscribe_id = ? AND user_id = ? AND "+day+" >= ? AND "+day+" < ?",
			userSubscribeID, userID, fromDay, toDay).
		Group(day).
		Order(day + " ASC").
		Scan(&rows).Error
	return rows, err
}

func (m *trafficRollupRepo) ServerHourPoints(ctx context.Context, userSubscribeID, userID int64, start, end time.Time) ([]traffic.ServerTraffic, error) {
	var rows []traffic.ServerTraffic
	hourStart := rollupColumn(m.Conn, "hour_start")
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficHourly{}).
		Select(rollupColumn(m.Conn, "server_id")+", "+m.sumExpr("upload")+", "+m.sumExpr("download")).
		Where("user_subscribe_id = ? AND user_id = ? AND "+hourStart+" >= ? AND "+hourStart+" < ?",
			userSubscribeID, userID, start, end).
		Group("server_id").
		Scan(&rows).Error
	return rows, err
}

func (m *trafficRollupRepo) ServerDayPoints(ctx context.Context, userSubscribeID, userID int64, fromDay, toDay string) ([]traffic.ServerTraffic, error) {
	var rows []traffic.ServerTraffic
	day := rollupColumn(m.Conn, "day")
	err := m.Conn.WithContext(ctx).Model(&traffic.TrafficDaily{}).
		Select(rollupColumn(m.Conn, "server_id")+", "+m.sumExpr("upload")+", "+m.sumExpr("download")).
		Where("user_subscribe_id = ? AND user_id = ? AND "+day+" >= ? AND "+day+" < ?",
			userSubscribeID, userID, fromDay, toDay).
		Group("server_id").
		Scan(&rows).Error
	return rows, err
}

// sumExpr renders the dialect's SUM with the column's own name as the alias
// the target field scans from. PostgreSQL returns numeric for SUM(bigint);
// the cast keeps it an int64 there.
func (m *trafficRollupRepo) sumExpr(column string) string {
	expr := "COALESCE(SUM(" + rollupColumn(m.Conn, column) + "), 0)"
	if m.Conn.Dialector.Name() == orm.DriverPostgres {
		return fmt.Sprintf("%s::bigint AS %s", expr, rollupColumn(m.Conn, column))
	}
	return expr + " AS " + rollupColumn(m.Conn, column)
}

// rollupColumn quotes a column of the table the statement names.
func rollupColumn(db *gorm.DB, column string) string {
	if db != nil && db.Statement != nil {
		return db.Statement.Quote(clause.Column{Name: column})
	}
	return column
}
