-- 术语统一：把「簇 cluster」的英文标识符改为「高频问题 frequent_question」，
-- 与运营能理解的中文术语一致。

ALTER TABLE content_question_clusters RENAME TO content_frequent_questions;
ALTER TABLE content_question_clusters_id_seq RENAME TO content_frequent_questions_id_seq;

ALTER TABLE content_topics RENAME COLUMN source_cluster_id TO source_frequent_question_id;
ALTER TABLE content_topics RENAME COLUMN source_cluster_question TO source_frequent_question_topic;
ALTER TABLE content_topics RENAME COLUMN source_cluster_frequency TO source_frequent_question_frequency;
