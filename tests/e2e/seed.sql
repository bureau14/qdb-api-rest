-- Legacy golden fixture (docs/e2e-plan.md, "Dataset"). Run by `make seed`
-- through common.sh::run_qdbsh_file: one qdbsh statement per line, `--` comments
-- and blank lines skipped, first failure aborts. Idempotent: every table is
-- dropped and recreated (DROP on a missing table and re-attaching a tag are
-- tolerated by the driver).
-- Never touches the `reproduce` dataset table.

-- Nine tiny tables with tags (ported from old master's rest-setup qdbsh_seed_db):
-- foo/bar/baz_01..03, values 1.0..27.0, tags tag_01..03 round-robin, all
-- three tags attached to $qdb.tagroot so /api/tags finds them.
DROP TABLE foo_01
CREATE TABLE foo_01 ($timestamp TIMESTAMP, value DOUBLE)
INSERT INTO foo_01 ($timestamp, value) VALUES (2020-01-01, 1.0), (2020-01-02, 2.0), (2020-01-03, 3.0)
attach_tag foo_01 tag_01
DROP TABLE foo_02
CREATE TABLE foo_02 ($timestamp TIMESTAMP, value DOUBLE)
INSERT INTO foo_02 ($timestamp, value) VALUES (2020-01-01, 4.0), (2020-01-02, 5.0), (2020-01-03, 6.0)
attach_tag foo_02 tag_02
DROP TABLE foo_03
CREATE TABLE foo_03 ($timestamp TIMESTAMP, value DOUBLE)
INSERT INTO foo_03 ($timestamp, value) VALUES (2020-01-01, 7.0), (2020-01-02, 8.0), (2020-01-03, 9.0)
attach_tag foo_03 tag_03
DROP TABLE bar_01
CREATE TABLE bar_01 ($timestamp TIMESTAMP, value DOUBLE)
INSERT INTO bar_01 ($timestamp, value) VALUES (2020-01-01, 10.0), (2020-01-02, 11.0), (2020-01-03, 12.0)
attach_tag bar_01 tag_01
DROP TABLE bar_02
CREATE TABLE bar_02 ($timestamp TIMESTAMP, value DOUBLE)
INSERT INTO bar_02 ($timestamp, value) VALUES (2020-01-01, 13.0), (2020-01-02, 14.0), (2020-01-03, 15.0)
attach_tag bar_02 tag_02
DROP TABLE bar_03
CREATE TABLE bar_03 ($timestamp TIMESTAMP, value DOUBLE)
INSERT INTO bar_03 ($timestamp, value) VALUES (2020-01-01, 16.0), (2020-01-02, 17.0), (2020-01-03, 18.0)
attach_tag bar_03 tag_03
DROP TABLE baz_01
CREATE TABLE baz_01 ($timestamp TIMESTAMP, value DOUBLE)
INSERT INTO baz_01 ($timestamp, value) VALUES (2020-01-01, 19.0), (2020-01-02, 20.0), (2020-01-03, 21.0)
attach_tag baz_01 tag_01
DROP TABLE baz_02
CREATE TABLE baz_02 ($timestamp TIMESTAMP, value DOUBLE)
INSERT INTO baz_02 ($timestamp, value) VALUES (2020-01-01, 22.0), (2020-01-02, 23.0), (2020-01-03, 24.0)
attach_tag baz_02 tag_02
DROP TABLE baz_03
CREATE TABLE baz_03 ($timestamp TIMESTAMP, value DOUBLE)
INSERT INTO baz_03 ($timestamp, value) VALUES (2020-01-01, 25.0), (2020-01-02, 26.0), (2020-01-03, 27.0)
attach_tag baz_03 tag_03
attach_tag tag_01 $qdb.tagroot
attach_tag tag_02 $qdb.tagroot
attach_tag tag_03 $qdb.tagroot

-- Every legacy column type plus both null sentinels: row 1 fully populated,
-- row 2 all null, row 3 mixed (nanosecond timestamp, negative int, string with
-- quote/comma/HTML characters -- the encoder has SetEscapeHTML(false)).
DROP TABLE legacy_types
CREATE TABLE legacy_types ($timestamp TIMESTAMP, b BLOB, i INT64, d DOUBLE, s STRING, y SYMBOL(legacy_sym), t TIMESTAMP)
INSERT INTO legacy_types ($timestamp, b, i, d, s, y, t) VALUES (2020-01-01T00:00:00Z, 'blob-1', 1, 1.5, 'str-1', 'sym-1', 2021-01-01T12:00:00Z)
INSERT INTO legacy_types ($timestamp, i) VALUES (2020-01-02T00:00:00Z, NULL)
INSERT INTO legacy_types ($timestamp, b, i, s, y) VALUES (2020-01-03T00:00:00.123456789Z, 'blob-3', -3, 's"quote,comma <&>', 'sym-3')

-- A column that is null in every row keeps the legacy type "none".
DROP TABLE legacy_allnull
CREATE TABLE legacy_allnull ($timestamp TIMESTAMP, i INT64)
INSERT INTO legacy_allnull ($timestamp, i) VALUES (2020-01-01, NULL)
