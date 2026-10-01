Each `<game>.gob.gz` here is PMT's copy of a game's dumps and wiki, used when the user has none of their own.
Nothing needs them refreshed: the engine reads the user's own dumps first, and a game with no file here works without it.
To refresh one, scan the game with both dump sets fresh, then copy `shipped-<game>.gob.gz` from PMT's cache folder here as `<game>.gob.gz`.
