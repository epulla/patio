-- Drop "# <name>" headings that load_skill round-trips piled up at the start of skill bodies.
UPDATE skill SET body = ltrim(substr(body, length(name) + 4), char(10) || char(13)) WHERE substr(body, 1, length(name) + 3) = '# ' || name || char(10);
UPDATE skill SET body = ltrim(substr(body, length(name) + 4), char(10) || char(13)) WHERE substr(body, 1, length(name) + 3) = '# ' || name || char(10);
UPDATE skill SET body = ltrim(substr(body, length(name) + 4), char(10) || char(13)) WHERE substr(body, 1, length(name) + 3) = '# ' || name || char(10);
UPDATE skill SET body = ltrim(substr(body, length(name) + 4), char(10) || char(13)) WHERE substr(body, 1, length(name) + 3) = '# ' || name || char(10);
UPDATE skill SET body = ltrim(substr(body, length(name) + 4), char(10) || char(13)) WHERE substr(body, 1, length(name) + 3) = '# ' || name || char(10);
UPDATE skill SET body = ltrim(substr(body, length(name) + 4), char(10) || char(13)) WHERE substr(body, 1, length(name) + 3) = '# ' || name || char(10);
UPDATE skill SET body = ltrim(substr(body, length(name) + 4), char(10) || char(13)) WHERE substr(body, 1, length(name) + 3) = '# ' || name || char(10);
UPDATE skill SET body = ltrim(substr(body, length(name) + 4), char(10) || char(13)) WHERE substr(body, 1, length(name) + 3) = '# ' || name || char(10);
