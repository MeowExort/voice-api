DROP INDEX IF EXISTS idx_read_states_user_id;
DROP INDEX IF EXISTS idx_message_mentions_user_id;
DROP INDEX IF EXISTS idx_emojis_server_id;
DROP INDEX IF EXISTS idx_stickers_pack_id;

DROP TABLE IF EXISTS read_states;
DROP TABLE IF EXISTS message_mentions;
DROP TABLE IF EXISTS emojis;
DROP TABLE IF EXISTS stickers;
DROP TABLE IF EXISTS sticker_packs;
