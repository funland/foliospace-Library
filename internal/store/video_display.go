package store

// Keep the file identity/title intact. Every file card receives the same resolved
// movie title as the work wall, while episode files retain their episode title.
const videoDisplayTitleSQL = `COALESCE(
 NULLIF((SELECT json_extract(o.value_json,'$') FROM video_metadata_overrides o WHERE o.video_id=videos.id AND o.field='title'),''),
 NULLIF((SELECT w.title FROM video_file_links l JOIN video_versions v ON v.id=l.version_id JOIN video_works w ON w.id=v.work_id WHERE l.video_id=videos.id AND w.kind='movie' ORDER BY w.id LIMIT 1),''),
 NULLIF((SELECT json_extract(m.local_json,'$.title') FROM video_metadata_inputs m WHERE m.video_id=videos.id),''),
 videos.title)`
