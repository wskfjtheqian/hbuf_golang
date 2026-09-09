package hcdc

import (
	"regexp"
	"strings"
)

// ParseDDL 解析 DDL 语句
func ParseDDL(schema Schema, sql string) *TableInfo {
	sql = strings.TrimSpace(sql)
	upperSQL := strings.ToUpper(sql)

	info := &TableInfo{
		Schema: schema,
	}

	if strings.Contains(upperSQL, "CREATE TABLE") {
		info.Action = "CREATE"
		info.IsCreate = true
		info.Table = extractTableName(sql)
		return info
	}

	if strings.Contains(upperSQL, "ALTER TABLE") {
		info.Action = "ALTER"
		info.IsAlter = true
		info.Table = extractTableName(sql)
		info.Columns = extractColumnChanges(sql)
		return info
	}

	if strings.Contains(upperSQL, "DROP TABLE") {
		info.Action = "DROP"
		info.IsDrop = true
		info.Table = extractTableName(sql)
		return info
	}

	return nil
}

// extractTableName 提取表名
func extractTableName(sql string) Table {
	re := regexp.MustCompile(`(?i)(?:create|alter|drop)\s+table\s+(?:` + "`" + `?([a-zA-Z0-9_]+)` + "`" + `?\.)?` + "`" + `?([a-zA-Z0-9_]+)` + "`" + `?`)
	matches := re.FindStringSubmatch(sql)
	if len(matches) >= 3 {
		return Table(matches[2])
	}
	return ""
}

// extractColumnChanges 提取 ALTER TABLE 中的列变更
func extractColumnChanges(sql string) []ColumnInfo {
	var changes []ColumnInfo

	// 1. 提取 ADD COLUMN
	addRe := regexp.MustCompile(`(?i)add\s+(?:column\s+)?` + "`" + `?([a-zA-Z0-9_]+)` + "`" + `?\s+([a-zA-Z0-9_]+)(\([^)]*\))?\s*(NOT\s+NULL)?\s*(?:DEFAULT\s+([^,\s]+))?\s*(?:COMMENT\s+'([^']*)')?`)
	addMatches := addRe.FindAllStringSubmatch(sql, -1)
	for _, m := range addMatches {
		change := ColumnInfo{
			Name:   Column(m[1]),
			Type:   m[2],
			Args:   m[3],
			Action: "ADD",
			IsNull: "YES",
		}
		if len(m) >= 5 && strings.Contains(strings.ToUpper(m[4]), "NOT NULL") {
			change.IsNull = "NO"
		}
		if len(m) >= 6 && m[5] != "" {
			change.Default = &m[5]
		}
		if len(m) >= 7 && m[6] != "" {
			change.Comment = m[6]
		}
		changes = append(changes, change)
	}

	// 2. 提取 MODIFY COLUMN
	modifyRe := regexp.MustCompile(`(?i)modify\s+(?:column\s+)?` + "`" + `?([a-zA-Z0-9_]+)` + "`" + `?\s+([a-zA-Z0-9_]+)(\([^)]*\))?\s*(NOT\s+NULL)?\s*(?:DEFAULT\s+([^,\s]+))?\s*(?:COMMENT\s+'([^']*)')?`)
	modifyMatches := modifyRe.FindAllStringSubmatch(sql, -1)
	for _, m := range modifyMatches {
		change := ColumnInfo{
			Name:   Column(m[1]),
			Type:   m[2],
			Args:   m[3],
			Action: "MODIFY",
			IsNull: "YES",
		}
		if len(m) >= 5 && strings.Contains(strings.ToUpper(m[4]), "NOT NULL") {
			change.IsNull = "NO"
		}
		if len(m) >= 6 && m[5] != "" {
			change.Default = &m[5]
		}
		if len(m) >= 7 && m[6] != "" {
			change.Comment = m[6]
		}
		changes = append(changes, change)
	}

	// 3. 提取 CHANGE COLUMN
	changeRe := regexp.MustCompile(`(?i)change\s+(?:column\s+)?` + "`" + `?([a-zA-Z0-9_]+)` + "`" + `?\s+` + "`" + `?([a-zA-Z0-9_]+)` + "`" + `?\s+([a-zA-Z0-9_]+)(\([^)]*\))?\s*(NOT\s+NULL)?\s*(?:DEFAULT\s+([^,\s]+))?\s*(?:COMMENT\s+'([^']*)')?`)
	changeMatches := changeRe.FindAllStringSubmatch(sql, -1)
	for _, m := range changeMatches {
		change := ColumnInfo{
			OldName: Column(m[1]),
			Name:    Column(m[2]),
			Type:    m[3],
			Args:    m[4],
			Action:  "CHANGE",
			IsNull:  "YES",
		}
		if len(m) >= 6 && strings.Contains(strings.ToUpper(m[5]), "NOT NULL") {
			change.IsNull = "NO"
		}
		if len(m) >= 7 && m[6] != "" {
			change.Default = &m[6]
		}
		if len(m) >= 8 && m[7] != "" {
			change.Comment = m[7]
		}
		changes = append(changes, change)
	}

	// 4. 提取 DROP COLUMN
	dropRe := regexp.MustCompile(`(?i)drop\s+(?:column\s+)?` + "`" + `?([a-zA-Z0-9_]+)` + "`" + `?`)
	dropMatches := dropRe.FindAllStringSubmatch(sql, -1)
	for _, m := range dropMatches {
		change := ColumnInfo{
			Name:   Column(m[1]),
			Action: "DROP",
		}
		changes = append(changes, change)
	}

	return changes
}
