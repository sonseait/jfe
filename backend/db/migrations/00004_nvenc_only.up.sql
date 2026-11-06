UPDATE settings SET value = (value - 'threads' - 'crf') || jsonb_build_object(
 'mode', 'disabled', 'cq', COALESCE(value->'crf', '23'::jsonb), 'device', 0
) WHERE key='encoding';
