(function () {
  const collectionList = document.getElementById("collection-list");
  const collectionEmpty = document.getElementById("collection-empty");
  const gameList = document.getElementById("game-list");
  const gameEmpty = document.getElementById("game-empty");
  const fieldList = document.getElementById("field-list");
  const fieldEmpty = document.getElementById("field-empty");
  const mediaList = document.getElementById("media-list");
  const mediaEmpty = document.getElementById("media-empty");
  const searchForm = document.getElementById("search-form");
  const searchInput = document.getElementById("search-input");
  const searchCollection = document.getElementById("search-collection");
  const searchClear = document.getElementById("search-clear");
  const addGameButton = document.getElementById("add-game");
  const toggleMissingButton = document.getElementById("toggle-missing-games");
  const editButton = document.getElementById("edit-game");
  const deleteButton = document.getElementById("delete-game");
  const deleteModal = document.getElementById("delete-modal");
  const deleteForm = document.getElementById("delete-form");
  const deleteClose = document.getElementById("delete-close");
  const deleteCancel = document.getElementById("delete-cancel");
  let deleteStatus = document.getElementById("delete-status");
  const deleteRemoveFiles = document.getElementById("delete-remove-files");
  const editCollectionButton = document.getElementById("edit-collection");
  const collectionTotalCount = document.getElementById("collection-total-count");
  const collectionModal = document.getElementById("collection-modal");
  const collectionForm = document.getElementById("collection-form");
  const collectionStatus = document.getElementById("collection-status");
  const collectionClose = document.getElementById("collection-close");
  const collectionCancel = document.getElementById("collection-cancel");
  const editModal = document.getElementById("edit-modal");
  const editForm = document.getElementById("edit-form");
  const editFields = document.getElementById("edit-fields");
  const editAddField = document.getElementById("edit-add-field");
  const editCancel = document.getElementById("edit-cancel");
  const editClose = document.getElementById("edit-close");
  const editStatus = document.getElementById("edit-status");
  const collectionSearchInput = document.getElementById("collection-search-input");
  const INDEX_FIELD_KEY = "x-index-id";
  const COLLECTION_FIELD_CONFIG = [
    { id: "collection-x-index-id", key: "x-index-id", readonly: true },
    { id: "collection-name", key: "collection" },
    { id: "collection-sortby", key: "sort-by", aliases: ["sortby"] },
    { id: "collection-extensions", key: "extensions", aliases: ["extension"] },
    { id: "collection-ignore-extensions", key: "ignore-extensions", aliases: ["ignore-extension"] },
    { id: "collection-ignore-files", key: "ignore-files", aliases: ["ignore-file"] },
    { id: "collection-files", key: "files", aliases: ["file"] },
    { id: "collection-regex", key: "regex" },
    { id: "collection-short-name", key: "shortname", aliases: ["short_name"] },
    { id: "collection-summary", key: "summary" },
    { id: "collection-description", key: "description", aliases: ["desc"] },
    { id: "collection-workdir", key: "workdir", aliases: ["cwd"] },
    { id: "collection-launch", key: "launch", aliases: ["command"] },
  ];
  const KNOWN_GAME_FIELDS = [
    "game",
    "sort-by",
    "sort_name",
    "sort_title",
    "file",
    "files",
    "developer",
    "developers",
    "publisher",
    "publishers",
    "genre",
    "genres",
    "tag",
    "tags",
    "summary",
    "description",
    "players",
    "release",
    "rating",
    "launch",
    "command",
    "workdir",
    "cwd",
    "assets.boxfront",
    "assets.boxback",
    "assets.boxspine",
    "assets.boxfull",
    "assets.cartridge",
    "assets.disc",
    "assets.cart",
    "assets.logo",
    "assets.marquee",
    "assets.bezel",
    "assets.screenshot",
    "assets.video",
  ];
  const rowState = new WeakMap();
  const duplicateRows = new Set();
  let removedFields = [];
  let editContext = null;
  let collectionEditContext = null;
  const collectionExtensions = new Map();

  let collections = [];
  let currentCollectionId = null;
  let currentGameId = null;
  let collectionSearchQuery = "";
  let showMissingGames = false;
  let searchQuery = "";
  let searchCollectionId = "";

  async function init() {
    try {
      const res = await fetch("/api/collections");
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`);
      }
      collections = await res.json();
      buildCollectionExtensionMap();
      populateCollectionFilterOptions();
      renderCollections();
      updateMissingToggleButton();
    } catch (err) {
      collectionEmpty.textContent = `加载合集失败: ${err.message}`;
      collectionEmpty.style.display = "block";
    }
  }

  function buildCollectionExtensionMap() {
    collectionExtensions.clear();
    collections.forEach((collection) => {
      if (!collection || !collection.id) {
        return;
      }
      if (!Array.isArray(collection.extensions)) {
        collectionExtensions.set(collection.id, []);
        return;
      }
      const normalized = collection.extensions
        .map((ext) => (ext == null ? "" : String(ext).trim().toLowerCase()))
        .filter((ext) => ext.length)
        .map((ext) => (ext.startsWith(".") ? ext : `.${ext}`));
      collectionExtensions.set(collection.id, normalized);
    });
  }

  function getCollectionExtensions(collectionId) {
    if (!collectionId) {
      return [];
    }
    return collectionExtensions.get(collectionId) || [];
  }

  function matchesCollectionSearch(collection, query) {
    if (!query) {
      return true;
    }
    const haystacks = [
      collection?.name,
      collection?.display_name,
      collection?.dir_name,
      collection?.relative_path,
    ];
    return haystacks.some((value) => (value || "").toLowerCase().includes(query));
  }

  function getCollectionCounts(collection) {
    const totalFallback = Array.isArray(collection?.games) ? collection.games.length : 0;
    const total = Number.isFinite(Number(collection?.total_games))
      ? Number(collection.total_games)
      : totalFallback;
    const availableFallback = Array.isArray(collection?.games)
      ? collection.games.filter((game) => !isMissingGame(game)).length
      : 0;
    const available = Number.isFinite(Number(collection?.available_games))
      ? Number(collection.available_games)
      : availableFallback;
    return { available, total };
  }

  function getGlobalCounts() {
    let available = 0;
    let total = 0;
    collections.forEach((collection) => {
      const counts = getCollectionCounts(collection);
      available += counts.available || 0;
      total += counts.total || 0;
    });
    return { available, total };
  }

  function getNextXIndex(collection) {
    if (!collection || !Array.isArray(collection.games) || !collection.games.length) {
      return 1;
    }
    const maxId = Math.max(
      ...collection.games.map((game) => Number(game.x_index_id) || 0),
    );
    return maxId + 1;
  }

  function buildDefaultFieldsForNewGame(collection, xIndexId) {
    const baseName = collection?.dir_name || "";
    return [
      { key: "x-index-id", values: [String(xIndexId)] },
      { key: "x-id", values: [baseName] },
      { key: "game", values: [""] },
      { key: "file", values: [""] },
      { key: "developer", values: ["none"] },
      { key: "publisher", values: ["none"] },
      { key: "assets.boxfront", values: [""] },
      { key: "assets.video", values: [""] },
    ];
  }

  function populateCollectionFilterOptions() {
    if (!searchCollection) {
      return;
    }
    searchCollection.innerHTML = "";
    const defaultOption = document.createElement("option");
    defaultOption.value = "";
    defaultOption.textContent = "全部合集";
    searchCollection.appendChild(defaultOption);
    collections.forEach((collection) => {
      const option = document.createElement("option");
      option.value = collection.id;
      option.textContent = collection.display_name || collection.name;
      searchCollection.appendChild(option);
    });
    if (searchCollectionId && collections.some((c) => c.id === searchCollectionId)) {
      searchCollection.value = searchCollectionId;
    } else {
      searchCollectionId = "";
      searchCollection.value = "";
    }
  }

  function renderCollections() {
    collections.sort(compareCollections);
    collectionList.innerHTML = "";
    const query = (collectionSearchQuery || "").trim().toLowerCase();
    const visibleCollections = collections.filter((collection) =>
      matchesCollectionSearch(collection, query),
    );
    updateCollectionTotalCount();
    if (!visibleCollections.length) {
      collectionEmpty.textContent = query ? "没有匹配的合集" : "未在目录中找到 metadata.pegasus.txt";
      collectionEmpty.style.display = "block";
      currentCollectionId = null;
      currentGameId = null;
      updateActionButtons();
      renderGames();
      return;
    }
    if (!currentCollectionId || !visibleCollections.some((c) => c.id === currentCollectionId)) {
      currentCollectionId = visibleCollections[0].id;
    }
    collectionEmpty.style.display = "none";
    visibleCollections.forEach((collection) => {
      const item = document.createElement("li");
      item.className = "list-item list-item-multiline";
      const nameLine = document.createElement("div");
      nameLine.className = "collection-name-line";
      const counts = getCollectionCounts(collection);
      const countBadge = document.createElement("span");
      countBadge.className = "collection-count";
      countBadge.textContent = `${counts.available}/${counts.total}`;
      const nameText = document.createElement("span");
      nameText.textContent = collection.name || collection.display_name || "";
      nameLine.appendChild(nameText);
      nameLine.appendChild(countBadge);
      const pathRow = document.createElement("div");
      pathRow.className = "collection-path-row";
      const pathLine = document.createElement("div");
      pathLine.className = "collection-path-line";
      pathLine.textContent = collection.relative_path || collection.dir_name || "";
      const extLine = document.createElement("div");
      extLine.className = "collection-ext-line";
      extLine.textContent = formatExtensions(collection.extensions);
      pathRow.appendChild(pathLine);
      pathRow.appendChild(extLine);
      item.appendChild(nameLine);
      item.appendChild(pathRow);
      if (collection.id === currentCollectionId) {
        item.classList.add("active");
      }
      item.addEventListener("click", () => {
        currentCollectionId = collection.id;
        currentGameId = null;
        if (searchQuery) {
          searchQuery = "";
          if (searchInput) {
            searchInput.value = "";
          }
        }
        if (searchCollectionId) {
          searchCollectionId = "";
          if (searchCollection) {
            searchCollection.value = "";
          }
        }
        renderCollections();
        renderGames();
        renderFields();
        renderMedia();
      });
      collectionList.appendChild(item);
    });
    renderGames();
  }

  function formatExtensions(exts) {
    if (!Array.isArray(exts) || !exts.length) {
      return "";
    }
    const display = exts.slice(0, 3).map((ext) => (ext || "").trim()).filter(Boolean);
    const suffix = exts.length > 3 ? " ..." : "";
    return display.join(", ") + suffix;
  }

  function compareCollections(a, b) {
    const aKey = normalizeCollectionSortKey(a);
    const bKey = normalizeCollectionSortKey(b);
    if (aKey === bKey) {
      return (a?.display_name || a?.name || "").localeCompare(b?.display_name || b?.name || "");
    }
    return aKey.localeCompare(bKey);
  }

  function normalizeCollectionSortKey(collection) {
    if (!collection) {
      return "";
    }
    return (collection.sort_key || collection.name || "").toLowerCase();
  }

  function getCurrentCollection() {
    return collections.find((c) => c.id === currentCollectionId) || null;
  }

  function findGameWithCollectionById(gameId) {
    if (!gameId) {
      return { game: null, collection: null };
    }
    for (const collection of collections) {
      const game = collection.games.find((g) => g.id === gameId);
      if (game) {
        return { game, collection };
      }
    }
    return { game: null, collection: null };
  }

  function getCurrentSelectionContext() {
    const { game, collection } = findGameWithCollectionById(currentGameId);
    if (!game || !collection) {
      return null;
    }
    return {
      game,
      collection,
      metadata_path: collection.metadata_path,
      x_index_id: game.x_index_id,
    };
  }

  function isMissingGame(game) {
    return Boolean(game && game.rom_missing);
  }

  function isAssetKey(key) {
    return Boolean(key) && key.toLowerCase().startsWith("assets.");
  }

  function isFileKey(key) {
    const lower = key ? key.toLowerCase() : "";
    return lower === "file" || lower === "files";
  }

  function isUploadableKey(key) {
    return isAssetKey(key) || isFileKey(key);
  }

  function assetNameFromKey(key) {
    if (!isAssetKey(key)) {
      return "";
    }
    return key.toLowerCase().replace(/^assets\./, "");
  }

  function shouldDisplayGame(game) {
    if (!game) {
      return false;
    }
    return showMissingGames || !isMissingGame(game);
  }

  function renderGames() {
    gameList.innerHTML = "";
    const query = (searchQuery || "").trim().toLowerCase();
    if (query) {
      renderSearchResults(query);
      return;
    }
    renderCollectionGames();
  }

  function renderSearchResults(query) {
    const matches = findMatchingGames(query);
    if (!matches.length) {
      gameEmpty.textContent = "没有匹配的游戏";
      gameEmpty.style.display = "block";
      currentGameId = null;
      renderFields();
      renderMedia();
      updateActionButtons();
      return;
    }
    gameEmpty.style.display = "none";
    if (!currentGameId || !matches.some((m) => m.game.id === currentGameId)) {
      currentGameId = matches[0].game.id;
      currentCollectionId = matches[0].collection.id;
    }
    matches.forEach(({ collection, game }) => {
      const item = createGameListItem(game, collection, () => {
        currentGameId = game.id;
        currentCollectionId = collection.id;
        renderGames();
        renderFields();
        renderMedia();
        renderCollections();
      });
      if (game.id === currentGameId) {
        item.classList.add("active");
      }
      gameList.appendChild(item);
    });
    renderFields();
    renderMedia();
    updateActionButtons();
  }

  function renderCollectionGames() {
    const coll = getCurrentCollection();
    if (!coll) {
      gameEmpty.textContent = "请选择左侧的合集";
      gameEmpty.style.display = "block";
      currentGameId = null;
      renderFields();
      renderMedia();
      updateActionButtons();
      return;
    }
    const visibleGames = (coll.games || []).filter((game) => shouldDisplayGame(game));
    if (!visibleGames.length) {
      gameEmpty.textContent = showMissingGames ? "该合集暂无游戏" : "该合集暂无可用游戏";
      gameEmpty.style.display = "block";
      currentGameId = null;
      renderFields();
      renderMedia();
      updateActionButtons();
      return;
    }
    gameEmpty.style.display = "none";
    if (!currentGameId || !visibleGames.some((g) => g.id === currentGameId)) {
      currentGameId = visibleGames[0].id;
    }
    visibleGames.forEach((game) => {
      const item = createGameListItem(game, coll, () => {
        currentGameId = game.id;
        renderGames();
        renderFields();
        renderMedia();
      });
      if (game.id === currentGameId) {
        item.classList.add("active");
      }
      gameList.appendChild(item);
    });
    renderFields();
    renderMedia();
    updateActionButtons();
  }

  function buildMediaPrefix(game) {
    const boxEmoji = game?.has_boxart ? "🎨" : "🚫";
    const videoEmoji = game?.has_video ? "🎞️" : "🚫";
    return `${boxEmoji}${videoEmoji} `;
  }

  function normalizeRomPath(path) {
    return path || "";
  }

  function buildNameLine(prefix, nameText) {
    const name = nameText || "";
    return prefix + name;
  }

  function createGameListItem(game, collection, onSelect) {
    const item = document.createElement("li");
    item.className = "list-item list-item-multiline";
    if (isMissingGame(game)) {
      item.classList.add("missing-game");
    }
    const prefix = buildMediaPrefix(game);
    const nameLine = document.createElement("div");
    nameLine.className = "game-name-line";
    const nameText = document.createElement("span");
    nameText.className = "game-name-left";
    nameText.textContent = buildNameLine(prefix, buildNameText(game));
    nameLine.appendChild(nameText);
    if (isMissingGame(game)) {
      const missingFlag = document.createElement("span");
      missingFlag.className = "game-missing-flag";
      missingFlag.textContent = "⛔";
      nameLine.appendChild(missingFlag);
    }
    const pathLine = document.createElement("div");
    pathLine.className = "game-path-line";
    pathLine.textContent = normalizeRomPath(game.rel_rom_path || game.rom_path);
    if (isMissingGame(game)) {
      pathLine.classList.add("missing-path");
    }
    item.appendChild(nameLine);
    item.appendChild(pathLine);
    if (typeof onSelect === "function") {
      item.addEventListener("click", onSelect);
    }
    return item;
  }

  function buildNameText(game) {
    if (!game) {
      return "";
    }
    const text = game.display_name || game.title || "";
    return text.replace(/\s*\(.+\)\s*$/, "");
  }

  function setRowFeedback(row, message, isError) {
    const state = getRowState(row);
    if (!state || !state.feedbackEl) {
      return;
    }
    state.feedbackEl.textContent = message || "";
    if (message && isError) {
      state.feedbackEl.classList.add("error");
    } else {
      state.feedbackEl.classList.remove("error");
    }
  }

  function validateGameFieldsForSave(fields) {
    const gameField = findFieldInPayload(fields, "game");
    if (!hasNonEmptyValue(gameField)) {
      return "game 字段不能为空";
    }
    const fileField = findFieldInPayload(fields, "file") || findFieldInPayload(fields, "files");
    if (!hasNonEmptyValue(fileField)) {
      return "file 字段不能为空";
    }
    const boxField = findFieldInPayload(fields, "assets.boxfront");
    if (!hasNonEmptyValue(boxField)) {
      return "assets.boxfront 字段不能为空";
    }
    return "";
  }

  function findFieldInPayload(fields, key) {
    if (!Array.isArray(fields)) {
      return null;
    }
    const lower = (key || "").toLowerCase();
    return (
      fields.find((field) => field && typeof field.key === "string" && field.key.toLowerCase() === lower) ||
      null
    );
  }

  function hasNonEmptyValue(field) {
    if (!field || !Array.isArray(field.values)) {
      return false;
    }
    return field.values.some((value) => value && value.trim().length);
  }

  function findMatchingGames(query) {
    const matches = [];
    const scopes = searchCollectionId
      ? collections.filter((c) => c.id === searchCollectionId)
      : collections;
    scopes.forEach((collection) => {
      (collection.games || []).forEach((game) => {
        if (!shouldDisplayGame(game)) {
          return;
        }
        if (matchesQuery(game, query)) {
          matches.push({ collection, game });
        }
      });
    });
    return matches;
  }

  function matchesQuery(game, query) {
    const haystacks = [];
    haystacks.push(game.title || "");
    haystacks.push(game.display_name || "");
    haystacks.push(getFieldText(game, ["name", "game", "title"]));
    haystacks.push(getFieldText(game, ["desc", "description", "summary"]));
    haystacks.push(getFieldText(game, ["file", "files"]));
    return haystacks.some((text) => text.toLowerCase().includes(query));
  }

  function getFieldText(game, keys) {
    if (!game || !game.fields) {
      return "";
    }
    const lowerKeys = new Set(keys.map((k) => k.toLowerCase()));
    const values = [];
    game.fields.forEach((field) => {
      const key = (field.key || "").toLowerCase();
      if (lowerKeys.has(key)) {
        values.push(...(field.values || []));
      }
    });
    return values.join("\n");
  }

  function applyCollectionUpdate(updated) {
    if (!updated) {
      return;
    }
    const idx = collections.findIndex((c) => {
      if (updated.x_index_id && c.x_index_id) {
        return c.metadata_path === updated.metadata_path && c.x_index_id === updated.x_index_id;
      }
      return c.metadata_path === updated.metadata_path && c.index === updated.index;
    });
    if (idx === -1) {
      collections.push(updated);
    } else {
      const prevId = collections[idx] ? collections[idx].id : "";
      collections[idx] = updated;
      if (prevId && currentCollectionId === prevId) {
        currentCollectionId = updated.id;
      }
    }
    buildCollectionExtensionMap();
    populateCollectionFilterOptions();
  }

  function getUsedKeys() {
    const used = new Set();
    if (!editFields) {
      return used;
    }
    editFields.querySelectorAll(".edit-field-row").forEach((row) => {
      const key = (row.dataset.key || "").trim().toLowerCase();
      if (key) {
        used.add(key);
      }
    });
    return used;
  }

  function getRowState(row) {
    return rowState.get(row) || {};
  }

  function recordRemovedField(row) {
    if (!row) {
      return;
    }
    duplicateRows.delete(row);
    const key = (row.dataset.key || "").trim();
    if (!key) {
      return;
    }
    const state = getRowState(row);
    const valueArea = state.valueArea;
    const values =
      valueArea && typeof valueArea.value === "string"
        ? valueArea.value.replace(/\r/g, "").split("\n").map((v) => v.trim()).filter((v) => v.length)
        : [];
    removedFields.push({ key, values });
  }

  function updateRowKey(row, newKey, game) {
    const state = getRowState(row);
    const rawKey = (newKey || "").trim();
    const normalized = rawKey.toLowerCase();
    row.dataset.key = normalized;
    if (state.keyDisplay) {
      state.keyDisplay.textContent = normalized || "(未选择)";
      state.keyDisplay.title = normalized;
    }
    if (state.keySelect && state.keySelect.value !== rawKey) {
      state.keySelect.value = rawKey;
    }
    if (isUploadableKey(normalized)) {
      if (state.uploadControls) {
        state.uploadControls.classList.remove("hidden");
      }
      refreshAssetPreview(row, normalized, game);
    } else if (state.uploadControls) {
      state.uploadControls.classList.add("hidden");
      if (state.previewEl) {
        state.previewEl.innerHTML = "";
      }
    }
  }

  function refreshAssetPreview(row, key, game) {
    if (!isAssetKey(key)) {
      return;
    }
    const state = getRowState(row);
    const previewEl = state.previewEl;
    if (!previewEl || !game || !Array.isArray(game.assets)) {
      return;
    }
    const assetName = assetNameFromKey(key).toLowerCase();
    const asset = game.assets.find((item) => (item.name || "").toLowerCase() === assetName);
    previewEl.innerHTML = "";
    if (!asset) {
      return;
    }
    if (asset.type === "image") {
      const img = document.createElement("img");
      img.src = asset.url;
      img.alt = asset.name;
      previewEl.appendChild(img);
    } else if (asset.type === "video") {
      const video = document.createElement("video");
      video.src = asset.url;
      video.controls = true;
      video.preload = "metadata";
      previewEl.appendChild(video);
    } else {
      const link = document.createElement("a");
      link.href = asset.url;
      link.target = "_blank";
      link.rel = "noreferrer";
      link.textContent = asset.file_name || "查看文件";
      previewEl.appendChild(link);
    }
  }

  function renderAssetPreviewFromPayload(container, asset) {
    if (!container) {
      return;
    }
    container.innerHTML = "";
    if (!asset) {
      return;
    }
    if (asset.type === "image") {
      const img = document.createElement("img");
      img.src = asset.url;
      img.alt = asset.name || asset.file_name || "";
      container.appendChild(img);
      return;
    }
    if (asset.type === "video") {
      const video = document.createElement("video");
      video.src = asset.url;
      video.controls = true;
      video.preload = "metadata";
      container.appendChild(video);
      return;
    }
    const link = document.createElement("a");
    link.href = asset.url;
    link.target = "_blank";
    link.rel = "noreferrer";
    link.textContent = asset.file_name || "查看文件";
    container.appendChild(link);
  }

  function startRowUpload(row) {
    const key = (row.dataset.key || "").trim().toLowerCase();
    if (!isUploadableKey(key)) {
      setEditStatus("当前字段不支持上传", true);
      return;
    }
    const context = editContext || getCurrentSelectionContext();
    if (!context) {
      setEditStatus("请选择需要上传媒体的游戏", true);
      return;
    }
    const fileInput = document.createElement("input");
    fileInput.type = "file";
    if (isAssetKey(key)) {
      fileInput.accept = "image/*,video/*";
    } else if (isFileKey(key)) {
      const exts = getCollectionExtensions(context.collection.id);
      fileInput.accept = exts && exts.length ? exts.join(",") : "*/*";
    } else {
      fileInput.accept = "*/*";
    }
    fileInput.addEventListener("change", () => {
      if (fileInput.files && fileInput.files[0]) {
        uploadFileForRow(row, fileInput.files[0], key, context);
      }
    });
    fileInput.click();
  }

  async function uploadFileForRow(row, file, key, context) {
    setEditStatus("上传中...");
    const formData = new FormData();
    formData.append("metadata_path", context.metadata_path);
    formData.append("x_index_id", context.x_index_id);
    formData.append("field", key);
    formData.append("file", file);
    try {
      const res = await fetch("/api/games/upload", {
        method: "POST",
        body: formData,
      });
      if (!res.ok) {
        const text = await res.text();
        if (res.status === 409) {
          const duplicateError = new Error(text || "ROM 已存在");
          duplicateError.isDuplicate = true;
          throw duplicateError;
        }
        throw new Error(text || "上传失败");
      }
      const data = await res.json();
      const state = getRowState(row);
      if (state.valueArea && data.file_path) {
        state.valueArea.value = data.file_path;
      }
      if (state.previewEl) {
        if (data.asset) {
          renderAssetPreviewFromPayload(state.previewEl, data.asset);
        } else {
          state.previewEl.innerHTML = "";
        }
      }
      duplicateRows.delete(row);
      setRowFeedback(row, "", false);
      setEditStatus("上传成功");
    } catch (err) {
      if (err && err.isDuplicate) {
        duplicateRows.add(row);
        setRowFeedback(row, err.message || "ROM 已存在", true);
        setEditStatus(err.message || "ROM 已存在", true);
        return;
      }
      setEditStatus(err.message, true);
    }
  }

  function renderFields() {
    fieldList.innerHTML = "";
    const { game } = findGameWithCollectionById(currentGameId);
    if (!game) {
      fieldEmpty.textContent = searchQuery ? "请在搜索结果中选择游戏" : "请选择游戏查看字段";
      fieldEmpty.style.display = "block";
      return;
    }
    if (!game.fields || !game.fields.length) {
      fieldEmpty.textContent = "该游戏没有额外字段";
      fieldEmpty.style.display = "block";
      return;
    }
    fieldEmpty.style.display = "none";
    const orderedFields = [...game.fields].sort(fieldSortComparator);
    orderedFields.forEach((field) => {
      const row = document.createElement("div");
      row.className = "field-row";
      const key = document.createElement("div");
      key.className = "field-key";
      key.textContent = (field.key || "").toLowerCase();
      const value = document.createElement("div");
      value.className = "field-value";
      value.textContent = (field.values || []).join("\n");
      row.appendChild(key);
      row.appendChild(value);
      fieldList.appendChild(row);
    });
  }

  function renderMedia() {
    mediaList.innerHTML = "";
    const { game } = findGameWithCollectionById(currentGameId);
    if (!game) {
      mediaEmpty.textContent = searchQuery ? "请在搜索结果中选择游戏" : "请选择游戏查看媒体";
      mediaEmpty.style.display = "block";
      return;
    }
    if (!game.assets || !game.assets.length) {
      mediaEmpty.textContent = "该游戏没有媒体文件";
      mediaEmpty.style.display = "block";
      return;
    }
    mediaEmpty.style.display = "none";
    game.assets.forEach((asset) => {
      const card = document.createElement("div");
      card.className = "media-card";
      const title = document.createElement("strong");
      title.textContent = `${asset.name} (${asset.file_name || ""})`;
      card.appendChild(title);
      if (asset.type === "image") {
        const img = document.createElement("img");
        img.src = asset.url;
        img.alt = asset.name;
        card.appendChild(img);
      } else if (asset.type === "video") {
        const video = document.createElement("video");
        video.src = asset.url;
        video.controls = true;
        video.preload = "metadata";
        card.appendChild(video);
      } else {
        const link = document.createElement("a");
        link.href = asset.url;
        link.target = "_blank";
        link.rel = "noreferrer";
        link.textContent = "下载";
        card.appendChild(link);
      }
      mediaList.appendChild(card);
    });
  }

  function openEditModal(gameOverride, contextOverride) {
    if (!editModal || !editFields) {
      return;
    }
    const baseContext = contextOverride || getCurrentSelectionContext();
    if (!baseContext) {
      setEditStatus("请先选择一个游戏", true);
      return;
    }
    if (isMissingGame(baseContext.game)) {
      setEditStatus("该游戏缺少 ROM，无法编辑", true);
      return;
    }
    editContext = { ...baseContext };
    removedFields = [];
    populateEditFields(gameOverride || baseContext.game);
    setEditStatus("");
    editModal.classList.remove("hidden");
  }

  function closeEditModal() {
    if (editModal) {
      stopVideos(editModal);
      editModal.classList.add("hidden");
    }
    removedFields = [];
    editContext = null;
    duplicateRows.clear();
  }

  function openDeleteModal() {
    if (deleteModal) {
      deleteModal.classList.remove("hidden");
      setDeleteStatus("");
      if (deleteRemoveFiles) {
        deleteRemoveFiles.checked = false;
      }
    }
  }

  function closeDeleteModal() {
    if (deleteModal) {
      deleteModal.classList.add("hidden");
    }
    setDeleteStatus("");
  }

  function populateEditFields(game) {
    editFields.innerHTML = "";
    const fileField = findFieldByKey(game?.fields, "file") || findFieldByKey(game?.fields, "files");
    const multipleFileValues = fileField && fileField.values && fileField.values.length > 1;
    const fallback = { key: "game", values: [game && game.title ? game.title : ""] };
    const fields = game && Array.isArray(game.fields) && game.fields.length ? game.fields : [fallback];
    fields.sort(fieldSortComparator);
    fields.forEach((field) => {
      const keyLower = (field.key || "").toLowerCase();
      const isIndexField = keyLower === INDEX_FIELD_KEY;
      const isFileField = keyLower === "file" || keyLower === "files";
      const shouldLock = isIndexField || (isFileField && multipleFileValues);
      editFields.appendChild(
        createEditableFieldRow(field, {
          isNew: false,
          sourceGame: game,
          locked: shouldLock,
          allowRemove: !shouldLock,
        }),
      );
    });
  }

  function findFieldByKey(fields, key) {
    if (!Array.isArray(fields)) {
      return null;
    }
    const lower = key.toLowerCase();
    return (
      fields.find((field) => (field.key || "").toLowerCase() === lower) ||
      null
    );
  }

  function findFieldByKeyWithAliases(fields, key, aliases = []) {
    const direct = findFieldByKey(fields, key);
    if (direct) {
      return direct;
    }
    if (Array.isArray(aliases)) {
      for (const alias of aliases) {
        const match = findFieldByKey(fields, alias);
        if (match) {
          return match;
        }
      }
    }
    return null;
  }

  function createEditableFieldRow(field = { key: "", values: [] }, options = {}) {
    const row = document.createElement("div");
    row.className = "edit-field-row";
    const keyWrapper = document.createElement("div");
    keyWrapper.className = "edit-field-key-wrapper";
    const valueWrapper = document.createElement("div");
    valueWrapper.className = "edit-field-value-wrapper";
    const disabledKeys = new Set(
      (options.disabledKeys ? Array.from(options.disabledKeys) : []).map((k) => k.toLowerCase()),
    );
    const locked = Boolean(options.locked);
    const allowRemove = options.allowRemove !== false && !locked;
    const keyLower = (field.key || "").toLowerCase();

    let keyElement;
    if (options.isNew) {
      const select = document.createElement("select");
      select.className = "edit-field-key-select";
      const placeholder = document.createElement("option");
      placeholder.value = "";
      placeholder.textContent = "选择字段";
      select.appendChild(placeholder);
      KNOWN_GAME_FIELDS.forEach((name) => {
        const option = document.createElement("option");
        option.value = name;
        option.textContent = name;
        if (disabledKeys.has(name.toLowerCase())) {
          option.disabled = true;
        }
        select.appendChild(option);
      });
      select.addEventListener("change", () => {
        const previewGame = editContext?.game || getCurrentSelectionContext()?.game;
        updateRowKey(row, select.value, previewGame);
      });
      keyElement = select;
    } else {
      const display = document.createElement("div");
      display.className = "edit-field-key-display";
      display.textContent = field.key || "";
      display.title = field.key || "";
      keyElement = display;
    }

    const valueArea = document.createElement("textarea");
    valueArea.className = "edit-field-value";
    valueArea.placeholder = "多个值使用换行分隔";
    valueArea.value = (field.values || []).join("\n");
    if (keyLower === "description") {
      valueArea.classList.add("description");
    }
    if (locked) {
      valueArea.readOnly = true;
      valueArea.classList.add("readonly");
    }

    const uploadControls = document.createElement("div");
    uploadControls.className = "asset-upload-controls hidden";
    const uploadBtn = document.createElement("button");
    uploadBtn.type = "button";
    uploadBtn.textContent = "上传文件";
    uploadBtn.addEventListener("click", () => startRowUpload(row));
    const preview = document.createElement("div");
    preview.className = "asset-preview";
    uploadControls.appendChild(uploadBtn);
    uploadControls.appendChild(preview);

    let removeBtn = null;
    if (allowRemove) {
      removeBtn = document.createElement("button");
      removeBtn.type = "button";
      removeBtn.className = "remove-field";
      removeBtn.textContent = "删除";
      removeBtn.addEventListener("click", () => {
        recordRemovedField(row);
        row.remove();
      });
    }

    keyWrapper.appendChild(keyElement);
    valueWrapper.appendChild(valueArea);
    valueWrapper.appendChild(uploadControls);
    const feedback = document.createElement("div");
    feedback.className = "upload-feedback";
    valueWrapper.appendChild(feedback);

    row.appendChild(keyWrapper);
    row.appendChild(valueWrapper);
    if (removeBtn) {
      row.appendChild(removeBtn);
    } else {
      const placeholder = document.createElement("div");
      placeholder.className = "remove-placeholder";
      row.appendChild(placeholder);
    }

    row.dataset.key = field.key || "";
    rowState.set(row, {
      keySelect: options.isNew ? keyElement : null,
      keyDisplay: options.isNew ? null : keyElement,
      valueArea,
      uploadControls,
      previewEl: preview,
      feedbackEl: feedback,
    });
    updateRowKey(row, field.key || "", options.sourceGame || null);
    return row;
  }

  function gatherFieldPayload() {
    if (!editFields) {
      return [];
    }
    const rows = Array.from(editFields.querySelectorAll(".edit-field-row"));
    const payload = [];
    rows.forEach((row) => {
      const state = getRowState(row);
      const valueArea = state.valueArea;
      if (!valueArea) {
        return;
      }
      const keySelect = state.keySelect;
      const key = keySelect
        ? (keySelect.value || "").trim().toLowerCase()
        : (row.dataset.key || "").trim().toLowerCase();
      const rawValues = valueArea.value.replace(/\r/g, "").split("\n");
      const values = rawValues.map((v) => v.trim()).filter((v) => v.length);
      if (key) {
        payload.push({ key, values });
      }
    });
    return payload;
  }

  function setEditStatus(message, isError = false) {
    if (!editStatus) {
      return;
    }
    editStatus.textContent = message;
    editStatus.style.color = isError ? "#ff8a8a" : "var(--text-muted)";
  }

  function setDeleteStatus(message, isError = false) {
    if (!deleteStatus) {
      return;
    }
    deleteStatus.textContent = message;
    deleteStatus.style.color = isError ? "#ff8a8a" : "var(--text-muted)";
  }

  function setCollectionStatus(message, isError = false) {
    if (!collectionStatus) {
      return;
    }
    collectionStatus.textContent = message || "";
    collectionStatus.style.color = isError ? "#ff8a8a" : "var(--text-muted)";
  }

  function updateActionButtons() {
    const context = getCurrentSelectionContext();
    const hasSelection = Boolean(context);
    const isMissing = context ? isMissingGame(context.game) : false;
    const disableEdit = !hasSelection || isMissing;
    const disableDelete = !hasSelection || isMissing;
    if (editButton) {
      editButton.disabled = disableEdit;
      editButton.classList.toggle("disabled", disableEdit);
      editButton.title = isMissing ? "缺失 ROM 的游戏仅支持查看" : "";
    }
    if (deleteButton) {
      deleteButton.disabled = disableDelete;
      deleteButton.classList.toggle("disabled", disableDelete);
      deleteButton.title = isMissing ? "缺失 ROM 的游戏仅支持查看" : "";
    }
  }

  function updateMissingToggleButton() {
    if (!toggleMissingButton) {
      return;
    }
    toggleMissingButton.textContent = showMissingGames ? "隐藏缺失" : "显示缺失";
    toggleMissingButton.classList.toggle("active", showMissingGames);
  }

  function updateCollectionTotalCount() {
    if (!collectionTotalCount) {
      return;
    }
    const counts = getGlobalCounts();
    collectionTotalCount.textContent = counts.total > 0 ? `${counts.available}/${counts.total}` : "";
  }

  function stopVideos(container) {
    if (!container) {
      return;
    }
    container.querySelectorAll("video").forEach((video) => {
      try {
        video.pause();
        video.currentTime = 0;
      } catch (e) {
        // ignore pause errors
      }
    });
  }

  function openCollectionModal(collection) {
    if (!collectionModal) {
      return;
    }
    if (!collection) {
      setCollectionStatus("请选择需要编辑的合集", true);
      return;
    }
    collectionEditContext = {
      metadata_path: collection.metadata_path,
      x_index_id: collection.x_index_id,
      originalFields: Array.isArray(collection.fields)
        ? collection.fields.map((field) => ({
            key: field?.key || "",
            values: Array.isArray(field?.values) ? [...field.values] : [],
          }))
        : [],
    };
    populateCollectionForm(collection);
    setCollectionStatus("");
    collectionModal.classList.remove("hidden");
  }

  function closeCollectionModal() {
    if (collectionModal) {
      collectionModal.classList.add("hidden");
    }
    if (collectionForm) {
      collectionForm.reset();
    }
    collectionEditContext = null;
    setCollectionStatus("");
  }

  function populateCollectionForm(collection) {
    COLLECTION_FIELD_CONFIG.forEach((cfg) => {
      const el = document.getElementById(cfg.id);
      if (!el) {
        return;
      }
      const field = findFieldByKeyWithAliases(collection?.fields, cfg.key, cfg.aliases || []);
      el.value = field && Array.isArray(field.values) ? field.values.join("\n") : "";
      if (cfg.readonly) {
        el.readOnly = true;
        el.classList.add("readonly");
      } else {
        el.readOnly = false;
        el.classList.remove("readonly");
      }
    });
  }

  function gatherCollectionFieldPayload() {
    const payload = [];
    const pending = [];
    const handledKeys = new Set();
    COLLECTION_FIELD_CONFIG.forEach((cfg) => {
      const el = document.getElementById(cfg.id);
      if (!el) {
        return;
      }
      const raw = (el.value || "").replace(/\r/g, "");
      const values = raw
        .split("\n")
        .map((v) => v.trim())
        .filter((v) => v.length);
      const canonicalKey = (cfg.key || "").toLowerCase();
      handledKeys.add(canonicalKey);
      if (Array.isArray(cfg.aliases)) {
        cfg.aliases.forEach((alias) => handledKeys.add(String(alias || "").toLowerCase()));
      }
      if (values.length) {
        pending.push({ key: cfg.key, values });
      }
    });
    const original = Array.isArray(collectionEditContext?.originalFields)
      ? collectionEditContext.originalFields
      : [];
    original.forEach((field) => {
      if (!field || !field.key) {
        return;
      }
      const lower = field.key.toLowerCase();
      if (handledKeys.has(lower)) {
        return;
      }
      payload.push({
        key: field.key,
        values: Array.isArray(field.values) ? field.values.map((value) => value) : [],
      });
    });
    return payload.concat(pending);
  }

  function validateCollectionFieldsForSave(fields) {
    const collectionField = findFieldInPayload(fields, "collection");
    if (!hasNonEmptyValue(collectionField)) {
      return "name 字段不能为空";
    }
    return "";
  }

  async function handleEditSubmit(event) {
    event.preventDefault();
    if (duplicateRows.size > 0) {
      setEditStatus("存在重复的 ROM 文件，请重新上传", true);
      return;
    }
    const context = editContext || getCurrentSelectionContext();
    if (!context) {
      setEditStatus("请选择需要编辑的游戏", true);
      return;
    }
    const fieldsPayload = gatherFieldPayload();
    const validationError = validateGameFieldsForSave(fieldsPayload);
    if (validationError) {
      setEditStatus(validationError, true);
      return;
    }
    setEditStatus("保存中...");
    try {
      const endpoint = context.isNew ? "/api/games/create" : "/api/games/update";
      const body = {
        metadata_path: context.metadata_path,
        x_index_id: context.x_index_id,
        fields: fieldsPayload,
      };
      if (!context.isNew) {
        body.removed_fields = removedFields;
      }
      const res = await fetch(endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || "保存失败");
      }
      const data = await res.json();
      removedFields = [];
      editContext = null;
      closeEditModal();
      applyCollectionUpdate(data.collection);
      if (data.collection && data.collection.id) {
        currentCollectionId = data.collection.id;
      }
      if (data.game && data.game.id) {
        currentGameId = data.game.id;
      }
      renderCollections();
      renderGames();
      renderFields();
      renderMedia();
      setEditStatus("保存成功");
    } catch (err) {
      setEditStatus(err.message, true);
    }
  }

  if (searchForm) {
    searchForm.addEventListener("submit", (event) => event.preventDefault());
  }
  if (collectionSearchInput) {
    collectionSearchInput.addEventListener("input", (event) => {
      collectionSearchQuery = event.target.value || "";
      renderCollections();
      renderGames();
      renderFields();
      renderMedia();
    });
  }
  if (searchInput) {
    searchInput.addEventListener("input", (event) => {
      searchQuery = event.target.value || "";
      renderGames();
    });
  }
  if (searchCollection) {
    searchCollection.addEventListener("change", (event) => {
      searchCollectionId = event.target.value || "";
      renderGames();
    });
  }
  if (searchClear) {
    searchClear.addEventListener("click", () => {
      searchQuery = "";
      searchCollectionId = "";
      if (searchInput) {
        searchInput.value = "";
      }
      if (searchCollection) {
        searchCollection.value = "";
      }
      renderCollections();
      renderGames();
      renderFields();
      renderMedia();
    });
  }

  if (toggleMissingButton) {
    toggleMissingButton.addEventListener("click", () => {
      showMissingGames = !showMissingGames;
      updateMissingToggleButton();
      const { game } = findGameWithCollectionById(currentGameId);
      if (!showMissingGames && isMissingGame(game)) {
        currentGameId = null;
      }
      renderCollections();
      renderFields();
      renderMedia();
    });
  }

  if (editCollectionButton) {
    editCollectionButton.addEventListener("click", () => {
      const collection = getCurrentCollection();
      if (!collection) {
        setCollectionStatus("请选择需要编辑的合集", true);
        return;
      }
      openCollectionModal(collection);
    });
  }

  if (collectionClose) {
    collectionClose.addEventListener("click", closeCollectionModal);
  }

  if (collectionCancel) {
    collectionCancel.addEventListener("click", (event) => {
      event.preventDefault();
      closeCollectionModal();
    });
  }

  if (collectionModal) {
    collectionModal.addEventListener("click", (event) => {
      if (event.target === collectionModal) {
        closeCollectionModal();
      }
    });
  }

  if (collectionForm) {
    collectionForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      if (!collectionEditContext) {
        setCollectionStatus("请选择需要编辑的合集", true);
        return;
      }
      const fieldsPayload = gatherCollectionFieldPayload();
      const validationError = validateCollectionFieldsForSave(fieldsPayload);
      if (validationError) {
        setCollectionStatus(validationError, true);
        return;
      }
      setCollectionStatus("保存中...");
      try {
        const res = await fetch("/api/collections/update", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            metadata_path: collectionEditContext.metadata_path,
            x_index_id: collectionEditContext.x_index_id,
            fields: fieldsPayload,
          }),
        });
        if (!res.ok) {
          const text = await res.text();
          throw new Error(text || "保存失败");
        }
        const data = await res.json();
        applyCollectionUpdate(data.collection);
        if (data.collection && data.collection.id) {
          currentCollectionId = data.collection.id;
        }
        renderCollections();
        renderGames();
        renderFields();
        renderMedia();
        closeCollectionModal();
      } catch (err) {
        setCollectionStatus(err.message || "保存失败", true);
      }
    });
  }

  if (addGameButton) {
    addGameButton.addEventListener("click", () => {
      const collection = getCurrentCollection();
      if (!collection) {
        setEditStatus("请选择需要添加游戏的合集", true);
        return;
      }
      const newId = getNextXIndex(collection);
      const defaultGame = {
        x_index_id: newId,
        fields: buildDefaultFieldsForNewGame(collection, newId),
        title: "",
        display_name: collection.dir_name || "",
        rel_rom_path: "",
      };
      const contextOverride = {
        metadata_path: collection.metadata_path,
        x_index_id: newId,
        collection,
        game: defaultGame,
        isNew: true,
      };
      openEditModal(defaultGame, contextOverride);
    });
  }
  if (editButton) {
    editButton.addEventListener("click", () => {
      const context = getCurrentSelectionContext();
      if (!context) {
        setEditStatus("请选择需要编辑的游戏", true);
        return;
      }
      if (isMissingGame(context.game)) {
        setEditStatus("缺失 ROM 的游戏仅支持查看，无法编辑", true);
        return;
      }
      openEditModal(context.game, { ...context, isNew: false });
    });
  }
  if (deleteButton) {
    deleteButton.addEventListener("click", () => {
      const context = getCurrentSelectionContext();
      if (!context) {
        setDeleteStatus("请选择需要删除的游戏", true);
        return;
      }
      if (isMissingGame(context.game)) {
        setDeleteStatus("缺失 ROM 的游戏仅支持查看，无法删除", true);
        return;
      }
      openDeleteModal();
    });
  }

  if (deleteClose) {
    deleteClose.addEventListener("click", closeDeleteModal);
  }
  if (deleteCancel) {
    deleteCancel.addEventListener("click", (event) => {
      event.preventDefault();
      closeDeleteModal();
    });
  }
  if (deleteForm) {
    deleteForm.addEventListener("submit", async (event) => {
      event.preventDefault();
      const context = getCurrentSelectionContext();
      if (!context) {
        setDeleteStatus("请选择需要删除的游戏", true);
        return;
      }
      setDeleteStatus("删除中...");
      try {
        const res = await fetch("/api/games/delete", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            metadata_path: context.metadata_path,
            x_index_id: context.x_index_id,
            remove_files: deleteRemoveFiles ? deleteRemoveFiles.checked : false,
          }),
        });
        if (!res.ok) {
          const text = await res.text();
          throw new Error(text || "删除失败");
        }
        const data = await res.json();
        removedFields = [];
        closeEditModal();
        closeDeleteModal();
        applyCollectionUpdate(data.collection);
        const updatedCollection =
          data.collection && data.collection.id
            ? collections.find((c) => c.id === data.collection.id)
            : null;
        if (updatedCollection && updatedCollection.games.length) {
          currentCollectionId = updatedCollection.id;
          currentGameId = updatedCollection.games[0].id;
        } else {
          currentGameId = null;
        }
        renderCollections();
        renderGames();
        renderFields();
        renderMedia();
        setDeleteStatus("删除成功");
      } catch (err) {
        setDeleteStatus(err.message, true);
      }
    });
  }
  if (editAddField) {
    editAddField.addEventListener("click", () => {
      if (editFields) {
        const used = getUsedKeys();
        const available = KNOWN_GAME_FIELDS.filter((name) => !used.has(name.toLowerCase()));
        if (!available.length) {
          setEditStatus("所有字段均已存在", true);
          return;
        }
        editFields.appendChild(
          createEditableFieldRow({ key: "", values: [] }, { isNew: true, disabledKeys: used }),
        );
      }
    });
  }
  if (editCancel) {
    editCancel.addEventListener("click", () => {
      closeEditModal();
    });
  }
  if (editClose) {
    editClose.addEventListener("click", closeEditModal);
  }
  if (editModal) {
    editModal.addEventListener("click", (event) => {
      if (event.target === editModal) {
        closeEditModal();
      }
    });
  }
  if (editForm) {
    editForm.addEventListener("submit", handleEditSubmit);
  }
  init();
})();
  function fieldSortComparator(a, b) {
    if (!a || !b) {
      return 0;
    }
    const aKey = (a.key || "").toLowerCase();
    const bKey = (b.key || "").toLowerCase();
    const order = ["x-index-id", "x-id", "game", "file", "files"];
    const aIndex = order.indexOf(aKey);
    const bIndex = order.indexOf(bKey);
    if (aIndex !== -1 || bIndex !== -1) {
      if (aIndex === -1) {
        return 1;
      }
      if (bIndex === -1) {
        return -1;
      }
      if (aIndex !== bIndex) {
        return aIndex - bIndex;
      }
    }
    const aIsAsset = aKey.startsWith("assets.");
    const bIsAsset = bKey.startsWith("assets.");
    if (aIsAsset !== bIsAsset) {
      return aIsAsset ? 1 : -1;
    }
    return aKey.localeCompare(bKey);
  }
