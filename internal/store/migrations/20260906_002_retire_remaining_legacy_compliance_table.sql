-- Complete retirement of the legacy Compliance Guard storage. The first
-- retirement migration intentionally handled the dependent tables first;
-- this table was missed from that list and has no inbound business-table
-- references.
DROP TABLE IF EXISTS guard_local_standards;
