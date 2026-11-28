package hcdc

import (
	"strconv"
	"strings"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// validateCursor 校验游标的物理位置与 GTID 格式。
func validateCursor(cursor Cursor) error {
	_, _, _, err := parseCursor(cursor)
	return err
}

// parseCursor 解析游标的 binlog 文件前缀、文件序号和 GTID 集。
func parseCursor(cursor Cursor) (string, uint64, mysql.GTIDSet, error) {
	separator := strings.LastIndexByte(cursor.File, '.')
	serial, err := strconv.ParseUint(cursor.File[separator+1:], 10, 64)
	if separator <= 0 || err != nil || cursor.Position == 0 {
		return "", 0, nil, herror.NewError("CDC cursor requires a physical binlog file and position")
	}
	flavor := cursorFlavor(cursor)
	if flavor != mysql.MySQLFlavor && flavor != mysql.MariaDBFlavor {
		return "", 0, nil, herror.NewError("CDC cursor has an unsupported GTID flavor")
	}
	set, err := mysql.ParseGTIDSet(flavor, cursor.GTIDSet)
	if err != nil {
		return "", 0, nil, herror.WrapMsg(err, "parse CDC GTID set")
	}
	return cursor.File[:separator], serial, set, nil
}

// validateAdvance 按物理位点模式校验两个游标之间的推进关系。
func validateAdvance(previous, next Cursor) error {
	return validateCursorAdvance(previous, next, false)
}

// validateCursorAdvance 按指定定位模式校验游标推进。
func validateCursorAdvance(previous, next Cursor, gtidAutoPosition bool) error {
	return validateCursorProgress(previous, next, gtidAutoPosition, "")
}

// validateCursorProgress 校验物理位点和 GTID 的推进。
// 相邻有序记录提供 transactionID，检查点和稀疏的结构版本可跨越多个事务。
func validateCursorProgress(previous, next Cursor, gtidAutoPosition bool, transactionID string) error {
	sameMember := next.SourceIdentity == previous.SourceIdentity
	if (!sameMember && !gtidAutoPosition) || cursorFlavor(next) != cursorFlavor(previous) {
		return herror.NewError("CDC cursor changed source identity or GTID flavor")
	}
	previousBase, previousSerial, previousSet, err := parseCursor(previous)
	if err != nil {
		return err
	}
	nextBase, nextSerial, nextSet, err := parseCursor(next)
	if err != nil {
		return err
	}
	if sameMember {
		if previousBase != nextBase {
			return herror.NewError("CDC cursor changed binlog basename within a generation")
		}
		if nextSerial < previousSerial || (nextSerial == previousSerial &&
			(next.File != previous.File || next.Position <= previous.Position)) {
			return herror.NewError("CDC cursor must advance the physical binlog position")
		}
	}
	if !nextSet.Contain(previousSet) {
		return herror.NewError("CDC cursor GTID set does not include previously committed transactions")
	}
	if gtidAutoPosition {
		if err := validateGTIDCursor(previous, previousSet); err != nil {
			return err
		}
		if err := validateGTIDCursor(next, nextSet); err != nil {
			return err
		}
		if previousSet.Contain(nextSet) {
			return herror.NewError("CDC auto-position cursor must advance the committed GTID set")
		}
		if previousMaria, ok := previousSet.(*mysql.MariadbGTIDSet); ok {
			nextMaria := nextSet.(*mysql.MariadbGTIDSet)
			for domain, previousGTID := range previousMaria.Sets {
				nextGTID := nextMaria.Sets[domain]
				if nextGTID.SequenceNumber == previousGTID.SequenceNumber && nextGTID.ServerID != previousGTID.ServerID {
					return herror.NewError("CDC MariaDB GTID origin changed at an unchanged domain sequence")
				}
			}
		}
		if transactionID != "" {
			return validateGTIDTransaction(cursorFlavor(next), transactionID, previousSet, nextSet)
		}
	}
	return nil
}

// validateGTIDCursor 校验自动定位所需的源成员、物理位置和 GTID 集。
func validateGTIDCursor(cursor Cursor, set mysql.GTIDSet) error {
	if cursor.SourceIdentity == "" || cursor.Position < 4 || set.IsEmpty() {
		return herror.NewError("CDC auto-position requires a source member, a usable physical position and a nonempty GTID set")
	}
	switch position := set.(type) {
	case *mysql.MysqlGTIDSet:
		for _, tags := range *position {
			for _, intervals := range tags {
				for _, interval := range intervals {
					if interval.Start <= 0 {
						return herror.NewError("CDC auto-position requires positive MySQL GTID sequences")
					}
				}
			}
		}
	case *mysql.MariadbGTIDSet:
		// 驱动将同域位置归并为一个条目，重复域必须从原始位置识别。
		domains := make(map[uint32]struct{}, len(position.Sets))
		for entry := range strings.SplitSeq(cursor.GTIDSet, ",") {
			gtid, err := mysql.ParseMariadbGTID(entry)
			if err != nil {
				return herror.WrapMsg(err, "parse CDC MariaDB GTID position")
			}
			if _, exists := domains[gtid.DomainID]; exists {
				return herror.NewError("CDC auto-position requires one MariaDB GTID per domain")
			}
			if gtid.ServerID == 0 || gtid.SequenceNumber == 0 {
				return herror.NewError("CDC auto-position requires positive MariaDB GTID server IDs and sequences")
			}
			domains[gtid.DomainID] = struct{}{}
		}
	}
	return nil
}

// validateGTIDTransaction 校验 GTID 集恰好推进了当前记录对应的事务。
func validateGTIDTransaction(flavor, transactionID string, previous, next mysql.GTIDSet) error {
	transaction, err := mysql.ParseGTIDSet(flavor, transactionID)
	if err != nil || transaction.IsEmpty() || transaction.String() != transactionID {
		return herror.NewError("CDC auto-position record requires one canonical transaction GTID")
	}
	switch set := transaction.(type) {
	case *mysql.MysqlGTIDSet:
		if len(*set) != 1 {
			return herror.NewError("CDC auto-position record requires one MySQL GTID")
		}
		for _, tags := range *set {
			if len(tags) != 1 {
				return herror.NewError("CDC auto-position record requires one MySQL GTID")
			}
			for _, intervals := range tags {
				if len(intervals) != 1 || intervals[0].Start <= 0 || intervals[0].Stop-intervals[0].Start != 1 {
					return herror.NewError("CDC auto-position record requires one MySQL GTID")
				}
			}
		}
	case *mysql.MariadbGTIDSet:
		if len(set.Sets) != 1 {
			return herror.NewError("CDC auto-position record requires one MariaDB GTID")
		}
	}
	if previous.Contain(transaction) {
		return herror.NewError("CDC auto-position record repeated an already committed GTID")
	}
	expected := previous.Clone()
	if err := expected.Update(transactionID); err != nil {
		return herror.WrapMsg(err, "advance CDC transaction GTID")
	}
	if !expected.Equal(next) {
		return herror.NewError("CDC auto-position cursor must advance by exactly its transaction GTID")
	}
	return nil
}

// cursorFlavor 返回游标的 GTID 方言，未指定时使用 MySQL。
func cursorFlavor(cursor Cursor) string {
	if cursor.Flavor == "" {
		return mysql.MySQLFlavor
	}
	return cursor.Flavor
}
