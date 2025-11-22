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

  init();
})();
