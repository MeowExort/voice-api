DROP INDEX IF EXISTS idx_channels_server_id;
DROP INDEX IF EXISTS idx_members_user_id;
DROP INDEX IF EXISTS idx_members_server_id;
DROP INDEX IF EXISTS idx_messages_channel_id;

DROP TABLE IF EXISTS whisper_lists;
DROP TABLE IF EXISTS invites;
DROP TABLE IF EXISTS attachments;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS permission_overrides;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS member_roles;
DROP TABLE IF EXISTS members;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS servers;
DROP TABLE IF EXISTS users;
