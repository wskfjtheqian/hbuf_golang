package mysqlsource

import (
	"bytes"
	"compress/zlib"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	driver "github.com/go-sql-driver/mysql"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

// TestCompressedBinlogBounds 验证有限展开的事务与行事件可通过，超限或声明不实会阻塞。
func TestCompressedBinlogBounds(t *testing.T) {
	writer, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	defer writer.Close()
	commit := make([]byte, replication.EventHeaderSize+8)
	commit[4] = byte(replication.XID_EVENT)
	binary.LittleEndian.PutUint32(commit[9:13], uint32(len(commit)))
	compressed := writer.EncodeAll(commit, nil)
	header := []byte{
		replication.OTW_PAYLOAD_SIZE_FIELD, 1, byte(len(compressed)),
		replication.OTW_PAYLOAD_COMPRESSION_TYPE_FIELD, 1, replication.ZSTD,
		replication.OTW_PAYLOAD_UNCOMPRESSED_SIZE_FIELD, 1, byte(len(commit)),
		replication.OTW_PAYLOAD_HEADER_END_MARK,
	}
	require.NoError(t, validateMySQLCompressedTransaction(append(bytes.Clone(header), compressed...)))
	header[8]++
	require.ErrorContains(t, validateMySQLCompressedTransaction(append(bytes.Clone(header), compressed...)), "differs from its header")
	header = append(header[:7], 4, 0xfd, 1, 0, 0x80, 0)
	require.ErrorContains(t, validateMySQLCompressedTransaction(append(header, compressed...)), "exceeds")

	var buffer bytes.Buffer
	zlibWriter := zlib.NewWriter(&buffer)
	_, err = zlibWriter.Write([]byte("MariaDB"))
	require.NoError(t, err)
	require.NoError(t, zlibWriter.Close())
	require.NoError(t, validateMariaDBCompressedData(append([]byte{1, 7}, buffer.Bytes()...)))
	require.ErrorContains(t, validateMariaDBCompressedData(append([]byte{1, 6}, buffer.Bytes()...)), "differs from its header")
	require.ErrorContains(t, validateMariaDBCompressedData(append([]byte{4, 0, 0x80, 0, 1}, buffer.Bytes()...)), "exceeds")
}

// TestSourceConsistentSnapshotIntegration 验证两表快照在并发写入下仍保持一致，并在增量阶段收到新事务。
func TestSourceConsistentSnapshotIntegration(t *testing.T) {
	for _, flavor := range []string{"mysql", "mariadb"} {
		t.Run(flavor, func(t *testing.T) {
			dsnName := "CDC_TEST_MYSQL_DSN"
			if flavor == "mariadb" {
				dsnName = "CDC_TEST_MARIADB_DSN"
			}
			dsn := os.Getenv(dsnName)
			if dsn == "" {
				if os.Getenv("CDC_TEST_REQUIRE_INTEGRATION") == "1" {
					t.Fatal("set " + dsnName + " to validate the source")
				}
				t.Skip("set " + dsnName + " to validate the source")
			}
			config, err := driver.ParseDSN(dsn)
			require.NoError(t, err)
			config.DBName = ""
			writer, err := sql.Open("mysql", config.FormatDSN())
			require.NoError(t, err)
			defer writer.Close()
			ctx := t.Context()
			schema := "cdc_snapshot_it_" + strconv.FormatInt(time.Now().UnixNano(), 10)
			_, err = writer.ExecContext(ctx, "CREATE DATABASE "+quoteIdentifier(schema))
			require.NoError(t, err)
			defer func() {
				_, err := writer.ExecContext(ctx, "DROP DATABASE "+quoteIdentifier(schema))
				if err != nil {
					t.Logf("drop isolated source schema: %v", err)
				}
			}()
			first := TableRef{Schema: schema, Name: "first_table"}
			second := TableRef{Schema: schema, Name: "second_table"}
			for _, ref := range []TableRef{first, second} {
				_, err := writer.ExecContext(ctx, "CREATE TABLE "+qualifiedTable(ref)+
					" (score INT NOT NULL, id INT NOT NULL PRIMARY KEY) ENGINE=InnoDB")
				require.NoError(t, err)
				_, err = writer.ExecContext(ctx, "INSERT INTO "+qualifiedTable(ref)+" VALUES (10,1)")
				require.NoError(t, err)
			}
			replica, err := Open(ctx, Config{
				Address: config.Addr, User: config.User, Password: config.Passwd, Flavor: flavor,
				SourceIdentity: schema, ServerID: uint32(60000 + time.Now().UnixNano()%6000), Tables: []TableRef{first, second},
			})
			require.NoError(t, err)
			defer replica.Close()
			snapshot, err := replica.BeginSnapshot(ctx)
			require.NoError(t, err)
			defer snapshot.Close()
			require.Len(t, snapshot.Tables, 2)

			scanScore := func(table Table) string {
				t.Helper()
				var scores []string
				err := snapshot.Scan(ctx, table, func(row []any) error {
					switch value := row[0].(type) {
					case []byte:
						scores = append(scores, string(value))
					default:
						scores = append(scores, fmt.Sprint(value))
					}
					return nil
				})
				require.NoError(t, err)
				require.Len(t, scores, 1)
				return scores[0]
			}
			require.Equal(t, "10", scanScore(snapshot.Tables[0]))
			transaction, err := writer.BeginTx(ctx, nil)
			require.NoError(t, err)
			defer transaction.Rollback()
			for _, ref := range []TableRef{first, second} {
				_, err := transaction.ExecContext(ctx, "UPDATE "+qualifiedTable(ref)+" SET score=20 WHERE id=1")
				require.NoError(t, err)
			}
			require.NoError(t, transaction.Commit())
			require.Equal(t, "10", scanScore(snapshot.Tables[1]))
			require.NoError(t, snapshot.Close())

			var emitted Transaction
			err = replica.Stream(ctx, snapshot.Cursor, snapshot.Tables, func(change Transaction) error {
				if len(change.Changes) == 0 {
					return nil
				}
				emitted = change
				return sql.ErrNoRows
			})
			require.ErrorIs(t, err, sql.ErrNoRows)
			require.Len(t, emitted.Changes, 2)
			for index, table := range []TableRef{first, second} {
				change := emitted.Changes[index]
				require.Equal(t, table, change.Table)
				require.Equal(t, int32(10), change.Before[0])
				require.Equal(t, int32(20), change.After[0])
			}
			require.Greater(t, emitted.Cursor.Position, snapshot.Cursor.Position)
		})
	}
}

type gtidResultExecutor struct{ result *mysql.Result }

// Execute 返回测试预设的 GTID 查询结果，供复制查询适配器测试使用。
func (e gtidResultExecutor) Execute(string, ...any) (*mysql.Result, error) { return e.result, nil }

// TestReplicationGTIDQueryOwnsResultsAfterClose 验证驱动关闭结果后复用缓冲时，已读取的 GTID 查询结果仍然有效。
func TestReplicationGTIDQueryOwnsResultsAfterClose(t *testing.T) {
	const file = "mariadb-bin.000001"
	column := []byte(file)
	result := mysql.NewResult(&mysql.Resultset{
		Fields: []*mysql.Field{{Name: []byte("Log_name")}},
		Values: [][]mysql.FieldValue{{mysql.NewFieldValue(mysql.FieldValueTypeString, 0, column)}},
	})
	values, err := replicationGTIDQuery(gtidResultExecutor{result})("SHOW BINARY LOGS")
	require.NoError(t, err)
	require.Nil(t, result.Resultset)
	// 驱动在关闭结果后可能为下一条查询复用这块底层内存。
	copy(column, "overwritten-result")
	require.Equal(t, []string{file}, values)
}

// TestMySQLGTIDResumeVerifiesExecutedAndPurgedSets 验证 MySQL 续传接受完整历史，并拒绝缺失或已清理的必要 GTID。
func TestMySQLGTIDResumeVerifiesExecutedAndPurgedSets(t *testing.T) {
	const sid = "1656a622-a170-4d00-b2f3-05d07c2441d8:"
	durable, err := mysql.ParseGTIDSet(mysql.MySQLFlavor, sid+"1-4")
	require.NoError(t, err)
	source := Source{config: Config{Flavor: mysql.MySQLFlavor}}
	for _, scenario := range []struct {
		name, executed, purged, wantError string
	}{
		{"ready", "1-7", "1-2", ""},
		{"missing", "1-3", "1-2", "persisted recovery GTID set"},
		{"purged", "1-7", "1-5", "purged binlog history"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			boundaries, err := source.verifyGTIDResume(func(query string, _ ...any) ([]string, error) {
				require.Contains(t, query, "@@GLOBAL.gtid_purged")
				return []string{sid + scenario.executed, sid + scenario.purged, "ON", "ON", "1", "0", "0"}, nil
			}, durable)
			if scenario.wantError == "" {
				require.NoError(t, err)
				require.Empty(t, boundaries)
				return
			}
			require.ErrorContains(t, err, scenario.wantError)
		})
	}
}

// TestMariaDBGTIDResumeRejectsConflictsAndRequiresUnprovenBoundaries 验证 MariaDB 续传拒绝来源冲突，并标出仍需从订阅流核验的 GTID 边界。
func TestMariaDBGTIDResumeRejectsConflictsAndRequiresUnprovenBoundaries(t *testing.T) {
	durable, err := mysql.ParseGTIDSet(mysql.MariaDBFlavor, "0-17-5,1-3-4")
	require.NoError(t, err)
	source := Source{config: Config{Flavor: mysql.MariaDBFlavor}}
	for _, scenario := range []struct {
		name, current, floor, wantError string
	}{
		{"advanced_member", "0-20-7,1-3-4", "0-17-2", ""},
		{"different_origin_at_same_sequence", "0-20-5,1-3-4", "0-17-2", "persisted recovery GTID set"},
		{"purged_required_history", "0-20-7,1-3-4", "0-20-6", "purged binlog history"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			boundaries, err := source.verifyGTIDResume(func(query string, arguments ...any) ([]string, error) {
				switch {
				case strings.Contains(query, "@@GLOBAL.gtid_binlog_pos"):
					return []string{scenario.current, scenario.current, scenario.current, "1", "1", "0"}, nil
				case query == "SHOW BINARY LOGS":
					return []string{"replica-bin.000002", "192"}, nil
				case query == "SELECT BINLOG_GTID_POS(?, 4)":
					require.Equal(t, []any{"replica-bin.000002"}, arguments)
					return []string{scenario.floor}, nil
				default:
					t.Fatalf("unexpected query %q", query)
					return nil, nil
				}
			}, durable)
			if scenario.wantError == "" {
				require.NoError(t, err)
				require.Equal(t, map[uint32]mysql.MariadbGTID{0: {DomainID: 0, ServerID: 17, SequenceNumber: 5}}, boundaries)
				return
			}
			require.ErrorContains(t, err, scenario.wantError)
		})
	}
}

// TestGTIDHandshakeBuffersEventsUntilExactMariaDBEvidence 验证 MariaDB 握手在核验精确 GTID 边界期间保留后续需要处理的事件。
func TestGTIDHandshakeBuffersEventsUntilExactMariaDBEvidence(t *testing.T) {
	stream := replication.NewBinlogStreamerWithChanSize(6)
	format := &replication.BinlogEvent{Header: &replication.EventHeader{}, Event: &replication.FormatDescriptionEvent{}}
	otherDomain := &replication.BinlogEvent{Header: &replication.EventHeader{EventSize: 50}, Event: &replication.XIDEvent{}}
	exact := mysql.MariadbGTID{DomainID: 7, ServerID: 5, SequenceNumber: 31}
	for _, event := range []*replication.BinlogEvent{
		{Header: &replication.EventHeader{}, Event: &replication.RotateEvent{Position: 4, NextLogName: []byte("new-primary.000003")}},
		format,
		{Header: &replication.EventHeader{}, Event: &replication.MariadbGTIDListEvent{GTIDs: []mysql.MariadbGTID{{DomainID: 7, ServerID: 9, SequenceNumber: 31}}}},
		otherDomain,
		{Header: &replication.EventHeader{}, Event: &replication.MariadbGTIDListEvent{GTIDs: []mysql.MariadbGTID{exact}}},
	} {
		require.NoError(t, stream.AddEventToStreamer(event))
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	file, buffered, err := confirmBinlogStream(ctx, stream, mysql.Position{}, map[uint32]mysql.MariadbGTID{7: exact}, nil)
	require.NoError(t, err)
	require.Equal(t, "new-primary.000003", file)
	require.Equal(t, []*replication.BinlogEvent{otherDomain}, buffered)
}
