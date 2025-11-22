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
  const editButton = document.getElementById("edit-game");
  const editModal = document.getElementById("edit-modal");
  const editForm = document.getElementById("edit-form");
  const editFields = document.getElementById("edit-fields");
  const editAddField = document.getElementById("edit-add-field");
  const editCancel = document.getElementById("edit-cancel");
  const editClose = document.getElementById("edit-close");
  const editStatus = document.getElementById("edit-status");
  const INDEX_FIELD_KEY = "x-index-id";
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
    "assets.boxart",
    "assets.boxfront",
    "assets.boxback",
    "assets.logo",
    "assets.banner",
    "assets.poster",
    "assets.clearlogo",
    "assets.marquee",
    "assets.screenshot",
    "assets.background",
    "assets.video",
  ];
  const rowState = new WeakMap();

  let collections = [];
  let currentCollectionId = null;
  let currentGameId = null;
  let searchQuery = "";
  let searchCollectionId = "";

  async function init() {
    try {
      const res = await fetch("/api/collections");
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`);
      }
      collections = await res.json();
      populateCollectionFilterOptions();
      renderCollections();
    } catch (err) {
      collectionEmpty.textContent = `加载合集失败: ${err.message}`;
      collectionEmpty.style.display = "block";
    }
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
    collectionList.innerHTML = "";
    if (!collections.length) {
      collectionEmpty.style.display = "block";
      return;
    }
    if (!currentCollectionId || !collections.some((c) => c.id === currentCollectionId)) {
      currentCollectionId = collections[0].id;
    }
    collectionEmpty.style.display = "none";
    collections.forEach((collection) => {
      const item = document.createElement("li");
      item.textContent = collection.display_name || collection.name;
      item.className = "list-item";
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

  function isAssetKey(key) {
    return Boolean(key) && key.toLowerCase().startsWith("assets.");
  }

  function assetNameFromKey(key) {
    if (!isAssetKey(key)) {
      return "";
    }
    return key.toLowerCase().replace(/^assets\./, "");
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
      return;
    }
    gameEmpty.style.display = "none";
    if (!currentGameId || !matches.some((m) => m.game.id === currentGameId)) {
      currentGameId = matches[0].game.id;
      currentCollectionId = matches[0].collection.id;
    }
    matches.forEach(({ collection, game }) => {
      const item = document.createElement("li");
      const labelParts = [];
      if (!searchCollectionId) {
        labelParts.push(collection.display_name || collection.name);
      }
      labelParts.push(game.display_name || game.title);
      item.textContent = labelParts.join(" · ");
      item.className = "list-item";
      if (game.id === currentGameId) {
        item.classList.add("active");
      }
      item.addEventListener("click", () => {
        currentGameId = game.id;
        currentCollectionId = collection.id;
        renderGames();
        renderFields();
        renderMedia();
        renderCollections();
      });
      gameList.appendChild(item);
    });
    renderFields();
    renderMedia();
  }

  function renderCollectionGames() {
    const coll = getCurrentCollection();
    if (!coll) {
      gameEmpty.textContent = "请选择左侧的合集";
      gameEmpty.style.display = "block";
      currentGameId = null;
      renderFields();
      renderMedia();
      return;
    }
    if (!coll.games.length) {
      gameEmpty.textContent = "该合集暂无游戏";
      gameEmpty.style.display = "block";
      currentGameId = null;
      renderFields();
      renderMedia();
      return;
    }
    gameEmpty.style.display = "none";
    if (!currentGameId || !coll.games.some((g) => g.id === currentGameId)) {
      currentGameId = coll.games[0].id;
    }
    coll.games.forEach((game) => {
      const item = document.createElement("li");
      item.textContent = game.display_name || game.title;
      item.className = "list-item";
      if (game.id === currentGameId) {
        item.classList.add("active");
      }
      item.addEventListener("click", () => {
        currentGameId = game.id;
        renderGames();
        renderFields();
        renderMedia();
      });
      gameList.appendChild(item);
    });
    renderFields();
    renderMedia();
  }

  function findMatchingGames(query) {
    const matches = [];
    const scopes = searchCollectionId
      ? collections.filter((c) => c.id === searchCollectionId)
      : collections;
    scopes.forEach((collection) => {
      collection.games.forEach((game) => {
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
    const idx = collections.findIndex(
      (c) => c.metadata_path === updated.metadata_path && c.index === updated.index,
    );
    if (idx === -1) {
      collections.push(updated);
    } else {
      collections[idx] = updated;
    }
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

  function updateRowKey(row, newKey, game) {
    const state = getRowState(row);
    const rawKey = (newKey || "").trim();
    const normalized = rawKey.toLowerCase();
    row.dataset.key = normalized;
    if (state.keyDisplay) {
      state.keyDisplay.textContent = rawKey || "(未选择)";
      state.keyDisplay.title = rawKey;
    }
    if (state.keySelect && state.keySelect.value !== rawKey) {
      state.keySelect.value = rawKey;
    }
    if (isAssetKey(normalized)) {
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
    if (!isAssetKey(key)) {
      setEditStatus("当前字段不支持上传", true);
      return;
    }
    const context = getCurrentSelectionContext();
    if (!context) {
      setEditStatus("请选择需要上传媒体的游戏", true);
      return;
    }
    const fileInput = document.createElement("input");
    fileInput.type = "file";
    fileInput.accept = "image/*,video/*";
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
    formData.append("file", file);
    try {
      const res = await fetch("/api/games/upload", {
        method: "POST",
        body: formData,
      });
      if (!res.ok) {
        const text = await res.text();
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
      setEditStatus("上传成功");
    } catch (err) {
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
    game.fields.forEach((field) => {
      const row = document.createElement("div");
      row.className = "field-row";
      const key = document.createElement("div");
      key.className = "field-key";
      key.textContent = field.key;
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

  function openEditModal() {
    if (!editModal || !editFields) {
      return;
    }
    const context = getCurrentSelectionContext();
    if (!context) {
      setEditStatus("请先选择一个游戏", true);
      return;
    }
    populateEditFields(context.game);
    setEditStatus("");
    editModal.classList.remove("hidden");
  }

  function closeEditModal() {
    if (editModal) {
      editModal.classList.add("hidden");
    }
  }

  function populateEditFields(game) {
    editFields.innerHTML = "";
    const fallback = { key: "game", values: [game && game.title ? game.title : ""] };
    const fields = game && Array.isArray(game.fields) && game.fields.length ? game.fields : [fallback];
    fields.forEach((field) => {
      const keyLower = (field.key || "").toLowerCase();
      const isIndexField = keyLower === INDEX_FIELD_KEY;
      editFields.appendChild(
        createEditableFieldRow(field, {
          isNew: false,
          sourceGame: game,
          locked: isIndexField,
          allowRemove: !isIndexField,
        }),
      );
    });
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
        updateRowKey(row, select.value, getCurrentSelectionContext()?.game);
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
        row.remove();
      });
    }

    keyWrapper.appendChild(keyElement);
    valueWrapper.appendChild(valueArea);
    valueWrapper.appendChild(uploadControls);

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
      const key = keySelect ? keySelect.value.trim().toLowerCase() : (row.dataset.key || "").trim();
      const rawValues = valueArea.value.replace(/\r/g, "").split("\n");
      const values = rawValues.map((v) => v.trim()).filter((v) => v.length);
      if (key) {
        payload.push({ key, values });
      }
    });
    return payload;
  }

  function ensureGameField(fields) {
    return fields.some(
      (field) => field.key.toLowerCase() === "game" && field.values && field.values.length,
    );
  }

  function setEditStatus(message, isError = false) {
    if (!editStatus) {
      return;
    }
    editStatus.textContent = message;
    editStatus.style.color = isError ? "#ff8a8a" : "var(--text-muted)";
  }

  async function handleEditSubmit(event) {
    event.preventDefault();
    const context = getCurrentSelectionContext();
    if (!context) {
      setEditStatus("请选择需要编辑的游戏", true);
      return;
    }
    const fieldsPayload = gatherFieldPayload();
    if (!fieldsPayload.length || !ensureGameField(fieldsPayload)) {
      setEditStatus("请至少保留 game 字段", true);
      return;
    }
    setEditStatus("保存中...");
    try {
      const res = await fetch("/api/games/update", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          metadata_path: context.metadata_path,
          x_index_id: context.x_index_id,
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
      if (data.game && data.game.id) {
        currentGameId = data.game.id;
      }
      renderCollections();
      renderGames();
      renderFields();
      renderMedia();
      setEditStatus("保存成功");
      setTimeout(() => closeEditModal(), 600);
    } catch (err) {
      setEditStatus(err.message, true);
    }
  }

  if (searchForm) {
    searchForm.addEventListener("submit", (event) => event.preventDefault());
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

  if (editButton) {
    editButton.addEventListener("click", openEditModal);
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
