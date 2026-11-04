-- M2: seed presets, record provider-call and usage metadata needed for the uncertain-outcome policy.
ALTER TABLE generation_jobs ADD COLUMN provider_call_started_at TIMESTAMPTZ;
ALTER TABLE generations ADD COLUMN provider_latency_ms INTEGER CHECK (provider_latency_ms >= 0);
ALTER TABLE generations ADD COLUMN usage JSONB;
CREATE INDEX generations_owner_status_idx ON generations(owner_id, status) WHERE status IN ('queued','processing');
INSERT INTO style_presets(id,name,category,description,prompt_template,template_version) VALUES
('old_money','Old Money','fashion','Classic, understated luxury: tailored cashmere, crisp shirts, neutral heritage tailoring.',
 'a classic "old money" outfit: tailored cashmere knitwear or a navy blazer, crisp oxford shirt, neutral cream, navy and camel tones, understated heritage elegance',1),
('minimalist','Minimalist','fashion','Clean lines, monochrome neutrals and architectural silhouettes.',
 'a minimalist outfit: clean lines, monochrome neutral palette (black, white, grey), architectural silhouette, premium drape, no logos',1),
('streetwear','Streetwear','fashion','Relaxed oversized cuts, technical textures and sneakers.',
 'a contemporary streetwear outfit: oversized hoodie or bomber jacket, relaxed cargo or wide-leg trousers, layered technical fabrics, designer sneakers',1),
('smart_casual','Smart Casual','fashion','Structured blazers with chic knitwear for day-to-evening.',
 'a smart casual outfit: structured blazer over a fine knit or open-collar shirt, tailored chinos or dark trousers, polished but relaxed',1),
('formal_elegant','Formal Elegant','fashion','Black tie sophistication with fine fabrics and immaculate tailoring.',
 'a formal black-tie outfit: a bespoke tuxedo or an elegant evening gown in rich fabrics such as silk or satin, immaculate tailoring',1);
