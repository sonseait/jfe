const errorCodes = new Set([
  'youtube_login_required',
  'youtube_unavailable',
  'youtube_request_failed',
  'youtube_partial',
  'youtube_forbidden',
  'youtube_rate_limited',
  'youtube_timeout',
  'youtube_network_failed',
  'youtube_tls_failed',
  'youtube_dependency_missing',
  'youtube_format_unavailable',
  'youtube_extraction_failed',
  'audio_storage_full',
  'audio_permission_denied',
]);

export function audioErrorKey(code: string) {
  return errorCodes.has(code) ? `audioUI.${code}` : 'audioUI.jobFailed';
}
