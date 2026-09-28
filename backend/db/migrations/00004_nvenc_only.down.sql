UPDATE settings SET value = (value - 'mode' - 'cq' - 'device') || jsonb_build_object(
 'threads', 2, 'crf', COALESCE(value->'cq', '23'::jsonb)
) WHERE key='encoding';
