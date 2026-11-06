-- name: RecommendationMovies :many
SELECT DISTINCT i.title, i.year, i.provider_id
FROM items i
WHERE i.kind='movie'
AND EXISTS (SELECT 1 FROM media_files f WHERE f.item_id=i.id AND f.available)
ORDER BY i.title, i.year, i.provider_id
LIMIT 10001;
