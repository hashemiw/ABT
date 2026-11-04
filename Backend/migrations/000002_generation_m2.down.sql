DELETE FROM style_presets WHERE id IN ('old_money','minimalist','streetwear','smart_casual','formal_elegant') AND NOT EXISTS (SELECT 1 FROM generations WHERE preset_id = style_presets.id);
DROP INDEX generations_owner_status_idx;
ALTER TABLE generations DROP COLUMN usage;
ALTER TABLE generations DROP COLUMN provider_latency_ms;
ALTER TABLE generation_jobs DROP COLUMN provider_call_started_at;
