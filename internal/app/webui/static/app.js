(function () {
  const collectionList = document.getElementById("collection-list");
  const collectionEmpty = document.getElementById("collection-empty");
  const gameList = document.getElementById("game-list");
  const gameEmpty = document.getElementById("game-empty");
  const fieldList = document.getElementById("field-list");
  const fieldEmpty = document.getElementById("field-empty");
  const mediaList = document.getElementById("media-list");
  const mediaEmpty = document.getElementById("media-empty");

  let collections = [];
  let currentCollectionId = null;
  let currentGameId = null;

  async function init() {
    try {
      const res = await fetch("/api/collections");
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`);
      }
      collections = await res.json();
      renderCollections();
    } catch (err) {
      collectionEmpty.textContent = `加载合集失败: ${err.message}`;
      collectionEmpty.style.display = "block";
    }
  }

  function renderCollections() {
    collectionList.innerHTML = "";
    if (!collections.length) {
      collectionEmpty.style.display = "block";
      return;
    }
    collectionEmpty.style.display = "none";
    if (!currentCollectionId && collections[0]) {
      currentCollectionId = collections[0].id;
    }
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

  function getCurrentGame() {
    const coll = getCurrentCollection();
    if (!coll) {
      return null;
    }
    return coll.games.find((g) => g.id === currentGameId) || null;
  }

  function renderGames() {
    gameList.innerHTML = "";
    const coll = getCurrentCollection();
    if (!coll) {
      gameEmpty.textContent = "请选择左侧的合集";
      gameEmpty.style.display = "block";
      return;
    }
    if (!coll.games.length) {
      gameEmpty.textContent = "该合集暂无游戏";
      gameEmpty.style.display = "block";
      return;
    }
    gameEmpty.style.display = "none";
    if (!currentGameId && coll.games[0]) {
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

  function renderFields() {
    fieldList.innerHTML = "";
    const game = getCurrentGame();
    if (!game) {
      fieldEmpty.textContent = "请选择游戏查看字段";
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
    const game = getCurrentGame();
    if (!game) {
      mediaEmpty.textContent = "请选择游戏查看媒体";
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

  init();
})();
