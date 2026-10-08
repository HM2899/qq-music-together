#!/usr/bin/env python3
# 来源：statindet/scripts/qqmusic/qqmusic_api.py（上游项目 LICENSE：GPL-3.0）。
# 保留下方 jixunmoe/qmweb-sign 的 MIT 参考声明；完整许可见项目第三方声明。
# 本项目变更：QQ 授权空昵称使用 session_from_credentials 的默认昵称。
"""JSON CLI shared by the QQ Music TUI and native Quickshell client."""

from __future__ import annotations

import argparse
import base64
import hashlib
import html
import http.cookiejar
import json
import os
import random
import re
import time
import urllib.parse
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any


MUSICU_URL = "https://u.y.qq.com/cgi-bin/musicu.fcg"
# 带签名的网关。网页版的评论写操作（发评论、删评论、点赞）都走这里，
# 请求体不变，URL 上多一个 zzc 签名（见 zzc_sign）。
MUSICS_URL = "https://u6.y.qq.com/cgi-bin/musics.fcg"
LYRIC_URL = "https://c.y.qq.com/lyric/fcgi-bin/fcg_query_lyric_new.fcg"
QR_SHOW_URL = "https://ssl.ptlogin2.qq.com/ptqrshow"
QR_LOGIN_URL = "https://ssl.ptlogin2.qq.com/ptqrlogin"
QQ_CHECK_SIG_URL = "https://ssl.ptlogin2.graph.qq.com/check_sig"
QQ_AUTHORIZE_URL = "https://graph.qq.com/oauth2.0/authorize"
FAVORITES_MAP_URL = "https://c.y.qq.com/splcloud/fcgi-bin/fcg_musiclist_getmyfav.fcg"
# 「我喜欢」的目录 ID。加/删收藏走 musicu.fcg 的 music.musicasset.PlaylistDetailWrite
# （见 mutate_favorite），经 musics.fcg 带签名发，和网页端一致。
# 老的 fcg_music_add2songdir.fcg / fcg_music_delbatchsong.fcg 服务端已废弃，
# 实测恒返回 403 invalid request / no permit，别再回头用。
FAVORITE_DIR_ID = 201
# v_songInfo 的 songType 是**歌曲自己的 type**（CgiGetTrackInfo 里的 type 字段，普通歌曲是 0），
# 网页端就是 {songType: song.type, songId: song.id}。
# 踩过的坑（2026-10-06 推翻旧结论）：以前走不带签名的 musicu.fcg，传 0 回 80105 且不生效，
# 于是误以为「只有 1 能写」并写死了 1；后来同一首歌传 1 回 1101、收藏全挂。
# 真正的条件是：带签名走 musics.fcg + songType 用歌曲的真实 type。
STATE_DIR = Path(os.environ.get("XDG_CACHE_HOME", Path.home() / ".cache")) / "quickshell" / "qqmusic"
SESSION_PATH = STATE_DIR / "session.json"
QR_STATE_PATH = STATE_DIR / "login-qr-state.json"
QR_IMAGE_PATH = STATE_DIR / "login-qr.png"
AVATAR_IMAGE_PATH = STATE_DIR / "avatar.jpg"
AVATAR_SOURCE_PATH = STATE_DIR / "avatar-source.txt"
PLAY_HISTORY_PATH = STATE_DIR / "play-history.json"
PLAYBACK_STATE_PATH = STATE_DIR / "playback-state.json"
WECHAT_QR_URL = "https://open.weixin.qq.com/connect/qrconnect"
WECHAT_QR_IMAGE_URL = "https://open.weixin.qq.com/connect/qrcode/{}"
WECHAT_QR_POLL_URL = "https://lp.open.weixin.qq.com/connect/l/qrconnect"
WECHAT_APP_ID = "wx48db31d50e334801"
SESSION_VALIDATION_TTL = 10 * 60
CREDENTIAL_FIELDS = (
    "openid",
    "refresh_token",
    "access_token",
    "expired_at",
    "expired_in",
    "musicid",
    "musickey",
    "unionid",
    "str_musicid",
    "refresh_key",
    "loginType",
    "musickeyCreateTime",
    "keyExpiresIn",
    "encryptUin",
)
HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
        "Chrome/124.0.0.0 Safari/537.36"
    ),
    "Referer": "https://y.qq.com/",
}
TAG_RE = re.compile(r"<[^>]+>")
TIMESTAMP_RE = re.compile(r"\[(\d{1,3}):(\d{2})(?:[\.:](\d{1,3}))?\]")
CALLBACK_VALUE_RE = re.compile(r"'((?:\\.|[^'])*)'")
WECHAT_UUID_RE = re.compile(r'uuid=(.+?)"')
WECHAT_STATUS_RE = re.compile(r"window\.wx_errcode=(\d+);window\.wx_code='([^']*)'")


class ApiError(RuntimeError):
    pass


class NoRedirectHandler(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req: Any, fp: Any, code: int, msg: str,
                         headers: Any, newurl: str) -> None:
        return None


def ensure_state_dir() -> None:
    STATE_DIR.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(STATE_DIR, 0o700)


def write_private_json(path: Path, payload: dict[str, Any]) -> None:
    ensure_state_dir()
    path.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")
    os.chmod(path, 0o600)


def read_private_json(path: Path) -> dict[str, Any]:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
        return payload if isinstance(payload, dict) else {}
    except (OSError, ValueError):
        return {}


def load_session() -> dict[str, Any]:
    session = read_private_json(SESSION_PATH)
    cookies = session.get("cookies")
    if not session.get("uin") or not isinstance(cookies, dict) or not cookies:
        return {}
    return session


def cookie_header(cookies: dict[str, Any]) -> str:
    return "; ".join(
        f"{key}={value}"
        for key, value in cookies.items()
        if key and value is not None
    )


def request_json(
    url: str,
    *,
    data: dict[str, Any] | None = None,
    form: dict[str, Any] | None = None,
    cookies: dict[str, Any] | None = None,
    extra_headers: dict[str, str] | None = None,
    timeout: float = 8.0,
) -> dict[str, Any]:
    body = None
    headers = dict(HEADERS)
    if extra_headers:
        headers.update(extra_headers)
    if cookies:
        headers["Cookie"] = cookie_header(cookies)
    if data is not None:
        body = json.dumps(data, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    elif form is not None:
        body = urllib.parse.urlencode(form).encode("utf-8")
        headers["Content-Type"] = "application/x-www-form-urlencoded"

    request = urllib.request.Request(url, data=body, headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            body = response.read()
            # 偶尔会碰到 GBK 编码的报错页，直接 utf-8 解码会炸成「请求失败」，
            # 把真正的错误信息吞掉，所以这里退一步解。
            try:
                text = body.decode("utf-8").strip()
            except UnicodeDecodeError:
                text = body.decode("gbk", errors="replace").strip()
            if text and not text.startswith(("{", "[")) and "(" in text:
                text = text[text.find("(") + 1 : text.rfind(")")]
            payload = json.loads(text)
            return payload if isinstance(payload, dict) else {"data": payload}
    except (OSError, ValueError, urllib.error.URLError) as error:
        raise ApiError(f"QQ 音乐服务请求失败: {error}") from error


def request_bytes(
    url: str,
    *,
    params: dict[str, Any] | None = None,
    extra_headers: dict[str, str] | None = None,
    timeout: float = 12.0,
) -> tuple[bytes, str]:
    target = url
    if params:
        target = f"{url}?{urllib.parse.urlencode(params)}"
    headers = dict(HEADERS)
    if extra_headers:
        headers.update(extra_headers)
    request = urllib.request.Request(target, headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.read(), str(response.headers.get("Content-Type") or "")
    except (OSError, urllib.error.URLError) as error:
        raise ApiError(f"登录服务请求失败: {error}") from error


def clean_text(value: Any) -> str:
    return html.unescape(TAG_RE.sub("", str(value or ""))).strip()


def cover_url(album_mid: str, size: int = 500) -> str:
    if not album_mid:
        return ""
    return (
        f"https://y.gtimg.cn/music/photo_new/T002R{size}x{size}M000"
        f"{urllib.parse.quote(album_mid)}.jpg"
    )


def singer_avatar_url(singer_mid: str, size: int = 300) -> str:
    """歌手头像。和专辑封面同一套 CDN 规则，只是 T001 = 歌手。"""
    mid = clean_text(singer_mid)
    if not mid:
        return ""
    return (
        f"https://y.gtimg.cn/music/photo_new/T001R{size}x{size}M000"
        f"{urllib.parse.quote(mid)}.jpg"
    )


def https_url(value: Any) -> str:
    """接口有时把封面给成 http://，桌面端 QML 加载会被混内容拦掉，统一升级成 https。"""
    text = clean_text(value)
    if text.startswith("http://"):
        return "https://" + text[len("http://") :]
    return text


def normalize_song(song: dict[str, Any], authenticated: bool = False) -> dict[str, Any]:
    track = song.get("track_info") if isinstance(song.get("track_info"), dict) else song
    singers = [
        clean_text(item.get("name"))
        for item in track.get("singer", [])
        if clean_text(item.get("name"))
    ]
    album_info = track.get("album") if isinstance(track.get("album"), dict) else {}
    album_mid = str(track.get("albummid") or album_info.get("mid") or "")
    album_name = clean_text(track.get("albumname") or album_info.get("name"))
    file_info = track.get("file") if isinstance(track.get("file"), dict) else {}
    pay_info = track.get("pay") if isinstance(track.get("pay"), dict) else {}
    vip = bool(int(pay_info.get("payplay") or pay_info.get("pay_play") or 0))
    song_mid = str(track.get("songmid") or track.get("mid") or "")

    return {
        "id": str(track.get("songid") or track.get("id") or song_mid),
        "songMid": song_mid,
        "mediaMid": str(file_info.get("media_mid") or song_mid),
        "title": clean_text(track.get("songname") or track.get("name") or track.get("title")),
        "artists": singers,
        "artist": " / ".join(singers) or "未知歌手",
        "album": album_name,
        "albumMid": album_mid,
        "duration": max(0, int(track.get("interval") or 0)),
        "coverUrl": cover_url(album_mid),
        "vip": vip,
        "playable": authenticated or not vip,
    }


def first_text(*values: Any) -> str:
    for value in values:
        text = clean_text(value)
        if text:
            return text
    return ""


def normalize_playlist(raw: dict[str, Any]) -> dict[str, Any]:
    playlist = raw.get("Playlist") if isinstance(raw.get("Playlist"), dict) else raw
    basic = playlist.get("basic") if isinstance(playlist.get("basic"), dict) else playlist
    cover = basic.get("cover")
    if isinstance(cover, dict):
        cover_value = first_text(
            cover.get("default_url"), cover.get("big_url"),
            cover.get("medium_url"), cover.get("small_url"), cover.get("pic_url2"),
        )
    else:
        cover_value = first_text(
            cover, basic.get("picurl"), basic.get("cover_url_big"),
            basic.get("cover_url_medium"), basic.get("picUrl"), basic.get("logo"),
            basic.get("imgurl"),
        )
    creator = basic.get("creator") if isinstance(basic.get("creator"), dict) else {}
    playlist_id = basic.get("tid") or basic.get("id") or basic.get("dissid")
    return {
        "id": str(playlist_id or ""),
        "kind": "playlist",
        "dirId": str(basic.get("dirid") or basic.get("dirId") or ""),
        "title": first_text(
            basic.get("title"), basic.get("dissname"), basic.get("dirName"),
            basic.get("name"),
        ) or "未命名歌单",
        "subtitle": first_text(basic.get("desc"), basic.get("description")),
        "coverUrl": https_url(cover_value),
        "songCount": max(
            0, int(basic.get("song_cnt") or basic.get("songNum")
                   or basic.get("songnum") or 0)
        ),
        "playCount": max(0, int(basic.get("play_cnt") or basic.get("listennum") or 0)),
        "creator": first_text(
            creator.get("nick"), basic.get("creator_nick"), basic.get("nick"),
            basic.get("nickname"), basic.get("username"),
        ),
    }


def normalize_envelope(raw: dict[str, Any], kind: str) -> dict[str, Any]:
    """把「歌单 / 专辑 / 歌手」三类搜索结果统一成一种信封。

    三者字段名完全不同（dissid/dissname、albumMID/albumName、singerMID/singerName），
    但对调用方来说都只是「一张带封面的卡片」，所以在这里对齐成长度一致的结构。
    """
    if kind == "album":
        album_mid = first_text(raw.get("albumMID"), raw.get("albumMid"))
        return {
            "id": album_mid,
            "kind": "album",
            "dirId": "",
            "title": first_text(raw.get("albumName")) or "未命名专辑",
            "subtitle": first_text(raw.get("publicTime"), raw.get("publishDate")),
            "coverUrl": https_url(first_text(raw.get("albumPic"))) or cover_url(album_mid),
            "songCount": max(0, int(raw.get("song_count") or raw.get("songNum") or 0)),
            "playCount": 0,
            "creator": first_text(raw.get("singerName")),
        }
    if kind == "songlist":
        creator = raw.get("creator") if isinstance(raw.get("creator"), dict) else {}
        return {
            "id": first_text(raw.get("dissid"), raw.get("docid")),
            "kind": "playlist",
            "dirId": "",
            "title": first_text(raw.get("dissname")) or "未命名歌单",
            "subtitle": first_text(raw.get("introduction")),
            "coverUrl": https_url(first_text(raw.get("imgurl"))),
            "songCount": max(0, int(raw.get("song_count") or 0)),
            "playCount": max(0, int(raw.get("listennum") or 0)),
            "creator": first_text(creator.get("name"), creator.get("nick")),
        }
    raise ApiError("未知的搜索结果类型: " + kind)


def normalize_singer(raw: dict[str, Any]) -> dict[str, Any]:
    singer_mid = first_text(raw.get("singerMID"), raw.get("mid"))
    return {
        "id": str(raw.get("singerID") or singer_mid),
        "mid": singer_mid,
        "name": first_text(raw.get("singerName"), raw.get("name")) or "未知歌手",
        "avatarUrl": https_url(first_text(raw.get("singerPic")))
        or singer_avatar_url(singer_mid),
        "songCount": max(0, int(raw.get("songNum") or 0)),
        "albumCount": max(0, int(raw.get("albumNum") or 0)),
        "mvCount": max(0, int(raw.get("mvNum") or 0)),
    }


SEARCH_TYPES = {
    "song": 0,
    "singer": 1,
    "album": 2,
    "songlist": 3,
}


def search_tracks(query: str, page: int, limit: int, kind: str = "song") -> dict[str, Any]:
    """搜索。kind 决定搜哪一类：song / singer / album / songlist。

    同一个接口（DoSearchForQQMusicDesktop）换 search_type 就能拿到四类结果，
    而且返回体里 body.{song,singer,album,songlist} 是**同时**给的——
    只是非当前类型的那几个是空数组。所以一类一次请求，不要指望一次拿全。
    """
    keyword = query.strip()
    search_type = SEARCH_TYPES.get(kind, 0)
    empty = {
        "ok": True, "query": "", "type": kind, "page": 1,
        "tracks": [], "singers": [], "albums": [], "playlists": [],
        "total": 0, "hasMore": False,
    }
    if not keyword:
        return empty

    current_page = max(1, page)
    page_size = max(1, min(limit, 50))
    session = load_session()
    cookies = session.get("cookies", {})
    uin = str(session.get("uin") or "0")

    # 旧的 soso/client_search_cp 已被服务端下线（恒返回 500），改走 musicu.fcg。
    # comm 必须用桌面端的 ct=19/cv=1859；换成 request_comm() 的 ct=24 会返回 0 条结果。
    payload = request_json(
        MUSICU_URL,
        data={
            "comm": {
                "ct": 19,
                "cv": 1859,
                "format": "json",
                "uin": int(uin) if uin.isdigit() else 0,
            },
            "req": {
                "module": "music.search.SearchCgiService",
                "method": "DoSearchForQQMusicDesktop",
                "param": {
                    "query": keyword,
                    "page_num": current_page,
                    "num_per_page": page_size,
                    "search_type": search_type,
                },
            },
        },
        cookies=cookies,
    )
    result = payload.get("req")
    if not isinstance(result, dict) or int(result.get("code") or 0) != 0:
        code = result.get("code") if isinstance(result, dict) else "无响应"
        raise ApiError(f"QQ 音乐搜索失败: {code}")
    data = result.get("data") if isinstance(result.get("data"), dict) else {}
    body = data.get("body") if isinstance(data.get("body"), dict) else {}
    meta = data.get("meta") if isinstance(data.get("meta"), dict) else {}

    section = body.get("song" if kind == "song" else "singer" if kind == "singer"
                       else "album" if kind == "album" else "songlist")
    raw_items = section.get("list") if isinstance(section, dict) else None
    if not isinstance(raw_items, list):
        raw_items = []

    authenticated = bool(session)
    tracks: list[dict[str, Any]] = []
    singers: list[dict[str, Any]] = []
    albums: list[dict[str, Any]] = []
    playlists: list[dict[str, Any]] = []
    if kind == "song":
        tracks = [
            normalize_song(item, authenticated=authenticated)
            for item in raw_items
            if isinstance(item, dict) and (item.get("songmid") or item.get("mid"))
        ]
        items = tracks
    elif kind == "singer":
        singers = [normalize_singer(item) for item in raw_items if isinstance(item, dict)]
        items = singers
    elif kind == "album":
        albums = [
            normalize_envelope(item, "album")
            for item in raw_items
            if isinstance(item, dict) and (item.get("albumMID") or item.get("albumMid"))
        ]
        items = albums
    else:
        playlists = [
            normalize_envelope(item, "songlist")
            for item in raw_items
            if isinstance(item, dict) and (item.get("dissid") or item.get("docid"))
        ]
        items = playlists

    # 分页总量只认 meta.sum。section 里那个 total 一直是 None（实测），别用它。
    # 歌曲类型的 sum 会被服务端截到 999，所以 hasMore 只能当「大概还有」用。
    total = int(meta.get("sum") or 0)
    if total <= 0:
        total = len(items)
    return {
        "ok": True,
        "query": keyword,
        "type": kind,
        "page": current_page,
        "tracks": tracks,
        "singers": singers,
        "albums": albums,
        "playlists": playlists,
        "total": total,
        "hasMore": bool(items) and current_page * page_size < total,
    }


def fetch_album(album_mid: str, page: int, limit: int) -> dict[str, Any]:
    """专辑详情 + 曲目。返回结构和 fetch_playlist 完全一致，调用方不用分两套。

    专辑没有「歌单 id」这种概念，但把 albumMid 放进 id、kind 标成 album，
    上层就能用同一套「打开详情」的逻辑处理歌单和专辑。
    """
    mid = album_mid.strip()
    if not mid:
        raise ApiError("缺少专辑标识")

    current_page = max(1, page)
    page_size = max(1, min(limit, 50))
    session = load_session()
    cookies = session.get("cookies", {})
    begin = (current_page - 1) * page_size

    songs_payload = request_json(
        MUSICU_URL,
        data={
            "comm": request_comm(session),
            "req": {
                "module": "music.musichallAlbum.AlbumSongList",
                "method": "GetAlbumSongList",
                "param": {"albumMid": mid, "begin": begin, "num": page_size, "order": 0},
            },
        },
        cookies=cookies,
    )
    songs_result = songs_payload.get("req")
    if not isinstance(songs_result, dict) or int(songs_result.get("code") or 0) != 0:
        code = songs_result.get("code") if isinstance(songs_result, dict) else "无响应"
        raise ApiError(f"专辑曲目获取失败: {code}")
    data = songs_result.get("data") if isinstance(songs_result.get("data"), dict) else {}
    raw_songs = data.get("songList") if isinstance(data.get("songList"), list) else []
    tracks = [
        normalize_song(item.get("songInfo"), authenticated=bool(session))
        for item in raw_songs
        if isinstance(item, dict) and isinstance(item.get("songInfo"), dict)
    ]
    total = max(0, int(data.get("totalNum") or len(tracks)))

    # 专辑元信息单独取一次。拿不到就退回「用第一首歌的专辑字段凑一个」，
    # 编排详情比整个操作失败好。
    basic: dict[str, Any] = {}
    try:
        info_payload = request_json(
            MUSICU_URL,
            data={
                "comm": request_comm(session),
                "req": {
                    "module": "music.musichallAlbum.AlbumInfoServer",
                    "method": "GetAlbumDetail",
                    "param": {"albumMid": mid},
                },
            },
            cookies=cookies,
        )
        info_result = info_payload.get("req")
        if isinstance(info_result, dict) and int(info_result.get("code") or 0) == 0:
            info_data = info_result.get("data")
            if isinstance(info_data, dict) and isinstance(info_data.get("basicInfo"), dict):
                basic = info_data["basicInfo"]
    except ApiError:
        basic = {}

    first = tracks[0] if tracks else {}
    singer = first.get("artist") or ""
    return {
        "ok": True,
        "playlist": {
            "id": mid,
            "kind": "album",
            "dirId": "",
            "title": first_text(basic.get("albumName"), first.get("album")) or "未命名专辑",
            "subtitle": first_text(basic.get("publishDate"), first_text(basic.get("desc"))[:60]),
            "coverUrl": cover_url(mid),
            "songCount": total,
            "playCount": 0,
            "creator": singer,
        },
        "page": current_page,
        "total": total,
        "tracks": tracks,
        "hasMore": bool(tracks) and begin + len(tracks) < total,
    }


# 音质档位：文件名前缀 + 扩展名，从高到低。拿不到所选档位时往下降。
# 实测（2026-10-06）：C400/M500/M800/F000 拿到的地址都能真正下载；
# RS01（Hi-Res）会给出地址但下载是 404，AI00（臻品母带）直接不给，所以都不提供。
QUALITY_LADDER: list[tuple[str, str, str]] = [
    ("flac", "F000", ".flac"),
    ("320", "M800", ".mp3"),
    ("128", "M500", ".mp3"),
    ("std", "C400", ".m4a"),
]


def resolve_track(song_mid: str, media_mid: str, quality: str = "") -> dict[str, Any]:
    if not song_mid:
        raise ApiError("缺少歌曲标识")
    if quality:
        return resolve_track_quality(song_mid, media_mid, quality)

    session = load_session()
    cookies = session.get("cookies", {})
    uin = str(session.get("uin") or "0")
    auth_key = str(cookies.get("qqmusic_key") or cookies.get("qm_keyst") or "")
    guid = str(random.SystemRandom().randint(1_000_000_000, 9_999_999_999))
    payload = {
        "req_0": {
            "module": "vkey.GetVkeyServer",
            "method": "CgiGetVkey",
            "param": {
                "guid": guid,
                "songmid": [song_mid],
                "songtype": [0],
                "uin": uin,
                "loginflag": 1,
                "platform": "20",
            },
        },
        "comm": {
            "uin": int(uin) if uin.isdigit() else 0,
            "format": "json",
            "ct": 24,
            "cv": 0,
            "authst": auth_key,
        },
    }
    response = request_json(MUSICU_URL, data=payload, cookies=cookies)
    data = response.get("req_0", {}).get("data", {})
    entries = data.get("midurlinfo", [])
    path = str(entries[0].get("purl") or "") if entries else ""
    servers = data.get("sip", [])
    if not path or not servers:
        raise ApiError("该歌曲需要 QQ 音乐会员或登录后播放")

    return {
        "ok": True,
        "songMid": song_mid,
        "mediaMid": media_mid or song_mid,
        "url": urllib.parse.urljoin(str(servers[0]), path),
    }


def resolve_track_quality(song_mid: str, media_mid: str, quality: str) -> dict[str, Any]:
    """按指定音质取播放地址；拿不到就逐档往下降，返回里的 quality 是实际拿到的档位。

    一次 CgiGetVkey 请求把「所选档位及以下」的文件名全带上，服务端对每个文件名各回一条，
    取第一条有 purl 的。文件名要用 media_mid（不是 song_mid）。全都没有时退回不带文件名的
    默认请求——和不选音质时的行为一致，最少能放。
    """
    keys = [key for key, _, _ in QUALITY_LADDER]
    if quality not in keys:
        raise ApiError("未知的音质：" + quality)
    ladder = QUALITY_LADDER[keys.index(quality):]
    media = media_mid or song_mid

    session = load_session()
    cookies = session.get("cookies", {})
    uin = str(session.get("uin") or "0")
    auth_key = str(cookies.get("qqmusic_key") or cookies.get("qm_keyst") or "")
    guid = str(random.SystemRandom().randint(1_000_000_000, 9_999_999_999))
    names = [prefix + media + ext for _, prefix, ext in ladder]
    payload = {
        "req_0": {
            "module": "vkey.GetVkeyServer",
            "method": "CgiGetVkey",
            "param": {
                "guid": guid,
                "songmid": [song_mid] * len(names),
                "songtype": [0] * len(names),
                "filename": names,
                "uin": uin,
                "loginflag": 1,
                "platform": "20",
            },
        },
        "comm": {
            "uin": int(uin) if uin.isdigit() else 0,
            "format": "json",
            "ct": 24,
            "cv": 0,
            "authst": auth_key,
        },
    }
    response = request_json(MUSICU_URL, data=payload, cookies=cookies)
    data = response.get("req_0", {}).get("data", {})
    servers = data.get("sip", [])
    by_name = {
        str(entry.get("filename") or ""): str(entry.get("purl") or "")
        for entry in data.get("midurlinfo", [])
        if isinstance(entry, dict)
    }
    if servers:
        for (key, _, _), name in zip(ladder, names):
            path = by_name.get(name, "")
            if path:
                return {
                    "ok": True,
                    "songMid": song_mid,
                    "mediaMid": media,
                    "url": urllib.parse.urljoin(str(servers[0]), path),
                    "quality": key,
                }
    result = resolve_track(song_mid, media_mid)
    result["quality"] = "default"
    return result


def decode_lyric(value: Any) -> str:
    raw = str(value or "")
    if not raw:
        return ""
    try:
        return base64.b64decode(raw, validate=True).decode("utf-8")
    except (ValueError, UnicodeDecodeError):
        return html.unescape(raw)


def parse_lrc(value: str) -> list[dict[str, Any]]:
    entries: list[dict[str, Any]] = []
    for raw_line in value.replace("\r", "").split("\n"):
        timestamps = list(TIMESTAMP_RE.finditer(raw_line))
        if not timestamps:
            continue
        text = TIMESTAMP_RE.sub("", raw_line).strip()
        if not text or text.lower().startswith(("offset:", "by:", "al:", "ti:", "ar:")):
            continue
        for timestamp in timestamps:
            fraction = timestamp.group(3) or "0"
            milliseconds = int(fraction.ljust(3, "0")[:3])
            seconds = int(timestamp.group(1)) * 60 + int(timestamp.group(2))
            entries.append(
                {
                    "time": round(seconds + milliseconds / 1000, 3),
                    "text": html.unescape(text),
                }
            )
    entries.sort(key=lambda item: item["time"])
    return entries


def fetch_lyrics(song_mid: str) -> dict[str, Any]:
    if not song_mid:
        raise ApiError("缺少歌曲标识")

    params = urllib.parse.urlencode(
        {
            "songmid": song_mid,
            "format": "json",
            "nobase64": 1,
            "g_tk": 5381,
        }
    )
    payload = request_json(f"{LYRIC_URL}?{params}")
    lyrics = parse_lrc(decode_lyric(payload.get("lyric")))
    translations = {
        round(item["time"], 3): item["text"]
        for item in parse_lrc(decode_lyric(payload.get("trans")))
    }
    for item in lyrics:
        item["translation"] = translations.get(round(item["time"], 3), "")

    if not lyrics:
        lyrics = [{"time": 0, "text": "暂无歌词", "translation": ""}]
    return {"ok": True, "songMid": song_mid, "lyrics": lyrics}


def cached_avatar_url(source_url: str) -> str:
    """Cache remote avatars because QML image loading may not have DNS access."""
    source = source_url.strip()
    parsed = urllib.parse.urlparse(source)
    if parsed.scheme not in {"http", "https"}:
        return source

    try:
        previous_source = AVATAR_SOURCE_PATH.read_text(encoding="utf-8").strip()
    except OSError:
        previous_source = ""
    if previous_source == source and AVATAR_IMAGE_PATH.is_file():
        return AVATAR_IMAGE_PATH.as_uri()

    try:
        request = urllib.request.Request(source, headers=HEADERS)
        with urllib.request.urlopen(request, timeout=10) as response:
            content_type = str(response.headers.get("Content-Type") or "")
            image = response.read(2 * 1024 * 1024 + 1)
        if not content_type.startswith("image/") or not image or len(image) > 2 * 1024 * 1024:
            raise OSError("头像响应不是有效图片")

        ensure_state_dir()
        AVATAR_IMAGE_PATH.write_bytes(image)
        AVATAR_SOURCE_PATH.write_text(source, encoding="utf-8")
        os.chmod(AVATAR_IMAGE_PATH, 0o600)
        os.chmod(AVATAR_SOURCE_PATH, 0o600)
        return AVATAR_IMAGE_PATH.as_uri()
    except (OSError, ValueError, urllib.error.URLError):
        return AVATAR_IMAGE_PATH.as_uri() if AVATAR_IMAGE_PATH.is_file() else source


def session_public_payload(session: dict[str, Any] | None = None) -> dict[str, Any]:
    current = session if session is not None else load_session()
    logged_in = bool(current.get("uin") and current.get("cookies"))
    uin = str(current.get("uin") or "") if logged_in else ""
    remote_avatar_url = str(current.get("avatarUrl") or (
        f"https://q1.qlogo.cn/g?b=qq&nk={urllib.parse.quote(uin)}&s=100"
        if uin and current.get("accountType") == "qq" else ""
    ))
    return {
        "ok": True,
        "loggedIn": logged_in,
        "uin": uin,
        "nickname": str(current.get("nickname") or (f"QQ {uin}" if uin else "")),
        "avatarUrl": cached_avatar_url(remote_avatar_url) if logged_in else "",
        "accountType": str(current.get("accountType") or ""),
    }


def stored_credentials(
    credentials: dict[str, Any],
    music_id: str,
    music_key: str,
    account_type: str,
) -> dict[str, Any]:
    result = {
        key: value
        for key in CREDENTIAL_FIELDS
        if (value := credentials.get(key)) not in (None, "")
        and isinstance(value, (str, int, float, bool))
    }
    result["musicid"] = music_id
    result["str_musicid"] = str(result.get("str_musicid") or music_id)
    result["musickey"] = music_key

    login_type = str(result.get("loginType") or credentials.get("login_type") or "")
    if login_type not in {"1", "2"}:
        if account_type == "wechat":
            login_type = "1"
        elif account_type == "qq":
            login_type = "2"
        else:
            login_type = "1" if music_key.startswith("W_X") else "2"
    result["loginType"] = int(login_type)
    return result


def session_from_credentials(
    credentials: dict[str, Any],
    account_type: str,
    *,
    nickname: str = "",
    base_cookies: dict[str, Any] | None = None,
) -> dict[str, Any]:
    music_id = str(
        credentials.get("musicid")
        or credentials.get("str_musicid")
        or credentials.get("qqmusic_uin")
        or ""
    )
    music_key = str(
        credentials.get("musickey")
        or credentials.get("qqmusic_key")
        or credentials.get("qm_keyst")
        or ""
    )
    if not music_id or not music_key:
        raise ApiError("QQ 音乐登录凭证不完整，请刷新二维码重试")

    cookies = dict(base_cookies or {})
    cookies.update(
        {
            "uin": f"o{music_id}",
            "qqmusic_uin": music_id,
            "qm_keyst": music_key,
            "qqmusic_key": music_key,
            "p_l_skey": music_key,
        }
    )
    optional_cookie_fields = {
        "euin": "encryptUin",
        "psrf_qqopenid": "openid",
        "psrf_qqaccess_token": "access_token",
        "psrf_qqrefresh_token": "refresh_token",
        "psrf_qqunionid": "unionid",
        "refresh_key": "refresh_key",
    }
    for cookie_name, field_name in optional_cookie_fields.items():
        field_value = credentials.get(field_name)
        if field_value:
            cookies[cookie_name] = str(field_value)

    credential_login_type = str(
        credentials.get("loginType") or credentials.get("login_type") or ""
    )
    if account_type == "qq" or credential_login_type == "2":
        cookies["login_type"] = "1"
    elif account_type == "wechat" or credential_login_type == "1":
        cookies["login_type"] = "2"
        cookies["wxuin"] = music_id

    now = int(time.time())
    return {
        "uin": music_id,
        "nickname": nickname or f"QQ 音乐 {music_id}",
        "accountType": account_type,
        "cookies": cookies,
        "credential": stored_credentials(
            credentials, music_id, music_key, account_type
        ),
        "createdAt": now,
        "validatedAt": now,
    }


def hash33(value: str) -> int:
    result = 0
    for character in value:
        result += (result << 5) + ord(character)
    return result & 0x7FFFFFFF


def gtk_token(cookies: dict[str, Any]) -> int:
    key = str(cookies.get("p_skey") or cookies.get("skey") or "")
    result = 5381
    for character in key:
        result += (result << 5) + ord(character)
    return result & 0x7FFFFFFF


def cookie_from_state(entry: dict[str, Any]) -> http.cookiejar.Cookie:
    domain = str(entry.get("domain") or ".qq.com")
    path = str(entry.get("path") or "/")
    return http.cookiejar.Cookie(
        version=0,
        name=str(entry.get("name") or ""),
        value=str(entry.get("value") or ""),
        port=None,
        port_specified=False,
        domain=domain,
        domain_specified=True,
        domain_initial_dot=domain.startswith("."),
        path=path,
        path_specified=True,
        secure=False,
        expires=None,
        discard=True,
        comment=None,
        comment_url=None,
        rest={},
        rfc2109=False,
    )


def serialize_cookie_jar(jar: http.cookiejar.CookieJar) -> list[dict[str, str]]:
    return [
        {
            "name": cookie.name,
            "value": cookie.value,
            "domain": cookie.domain or ".qq.com",
            "path": cookie.path or "/",
        }
        for cookie in jar
        if cookie.name and cookie.value
    ]


def start_qq_qr_login() -> dict[str, Any]:
    ensure_state_dir()
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    params = urllib.parse.urlencode(
        {
            "appid": 716027609,
            "e": 2,
            "l": "M",
            "s": 3,
            "d": 72,
            "v": 4,
            "t": f"{random.random():.16f}",
            "daid": 383,
            "pt_3rd_aid": 100497308,
        }
    )
    request = urllib.request.Request(
        f"{QR_SHOW_URL}?{params}",
        headers={**HEADERS, "Referer": "https://xui.ptlogin2.qq.com/"},
    )
    try:
        with opener.open(request, timeout=10) as response:
            image = response.read()
    except (OSError, urllib.error.URLError) as error:
        raise ApiError(f"获取登录二维码失败: {error}") from error

    cookies = serialize_cookie_jar(jar)
    if not any(item["name"] == "qrsig" for item in cookies):
        raise ApiError("QQ 登录服务没有返回二维码会话")

    QR_IMAGE_PATH.write_bytes(image)
    os.chmod(QR_IMAGE_PATH, 0o600)
    write_private_json(
        QR_STATE_PATH,
        {"type": "qq", "createdAt": int(time.time()), "cookies": cookies},
    )
    return {
        "ok": True,
        "type": "qq",
        "state": "waiting",
        "message": "等待 QQ 扫码",
        "qrPath": str(QR_IMAGE_PATH),
    }


def poll_qq_qr_login(state: dict[str, Any]) -> dict[str, Any]:
    cookie_entries = state.get("cookies")
    if not isinstance(cookie_entries, list) or not cookie_entries:
        return {"ok": True, "state": "expired", "message": "二维码已失效"}
    if int(time.time()) - int(state.get("createdAt") or 0) > 180:
        return {"ok": True, "state": "expired", "message": "二维码已失效"}

    jar = http.cookiejar.CookieJar()
    for entry in cookie_entries:
        if isinstance(entry, dict) and entry.get("name"):
            jar.set_cookie(cookie_from_state(entry))
    qrsig = next((cookie.value for cookie in jar if cookie.name == "qrsig"), "")
    if not qrsig:
        return {"ok": True, "state": "expired", "message": "二维码已失效"}

    params = urllib.parse.urlencode(
        {
            "u1": "https://graph.qq.com/oauth2.0/login_jump",
            "ptqrtoken": hash33(qrsig),
            "ptredirect": 0,
            "h": 1,
            "t": 1,
            "g": 1,
            "from_ui": 1,
            "ptlang": 2052,
            "action": f"0-0-{int(time.time() * 1000)}",
            "js_ver": 22080914,
            "js_type": 1,
            "login_sig": "",
            "pt_uistyle": 40,
            "aid": 716027609,
            "daid": 383,
            "pt_3rd_aid": 100497308,
        }
    )
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    request = urllib.request.Request(
        f"{QR_LOGIN_URL}?{params}",
        headers={**HEADERS, "Referer": "https://xui.ptlogin2.qq.com/"},
    )
    try:
        with opener.open(request, timeout=10) as response:
            text = response.read().decode("utf-8", errors="replace")
    except (OSError, urllib.error.URLError) as error:
        raise ApiError(f"检查登录状态失败: {error}") from error

    values = [item.replace("\\'", "'") for item in CALLBACK_VALUE_RE.findall(text)]
    code = values[0] if values else ""
    if code == "66":
        return {"ok": True, "state": "waiting", "message": "等待 QQ 扫码"}
    if code == "67":
        return {"ok": True, "state": "scanned", "message": "已扫码，等待确认"}
    if code in {"65", "68"}:
        return {"ok": True, "state": "expired", "message": "二维码已失效"}
    if code != "0" or len(values) < 3:
        message = values[4] if len(values) > 4 else "登录失败"
        return {"ok": True, "state": "error", "message": message}

    session = authorize_qq_qr(jar, values[2], values[5] if len(values) > 5 else "")
    write_private_json(SESSION_PATH, session)
    QR_STATE_PATH.unlink(missing_ok=True)
    QR_IMAGE_PATH.unlink(missing_ok=True)
    payload = session_public_payload(session)
    payload.update({"state": "success", "message": "登录成功", "type": "qq"})
    return payload


def authorize_qq_qr(
    jar: http.cookiejar.CookieJar,
    redirect_url: str,
    nickname: str,
) -> dict[str, Any]:
    redirect_params = urllib.parse.parse_qs(urllib.parse.urlparse(redirect_url).query)
    uin = str((redirect_params.get("uin") or [""])[0])
    sigx = str((redirect_params.get("ptsigx") or [""])[0])
    if not uin or not sigx:
        raise ApiError("QQ 登录服务没有返回授权参数，请刷新二维码重试")

    opener = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(jar), NoRedirectHandler()
    )
    check_params = urllib.parse.urlencode(
        {
            "uin": uin,
            "pttype": 1,
            "service": "ptqrlogin",
            "nodirect": 0,
            "ptsigx": sigx,
            "s_url": "https://graph.qq.com/oauth2.0/login_jump",
            "ptlang": 2052,
            "ptredirect": 100,
            "aid": 716027609,
            "daid": 383,
            "j_later": 0,
            "low_login_hour": 0,
            "regmaster": 0,
            "pt_login_type": 3,
            "pt_aid": 0,
            "pt_aaid": 16,
            "pt_light": 0,
            "pt_3rd_aid": 100497308,
        }
    )
    try:
        request = urllib.request.Request(
            f"{QQ_CHECK_SIG_URL}?{check_params}",
            headers={**HEADERS, "Referer": "https://xui.ptlogin2.qq.com/"},
        )
        with opener.open(request, timeout=12) as response:
            response.read(1)
    except urllib.error.HTTPError as error:
        if not 300 <= error.code < 400:
            raise ApiError(f"完成 QQ 登录失败: HTTP {error.code}") from error
    except (OSError, urllib.error.URLError) as error:
        raise ApiError(f"完成 QQ 登录失败: {error}") from error

    jar_cookies = {cookie.name: cookie.value for cookie in jar if cookie.name and cookie.value}
    p_skey = str(jar_cookies.get("p_skey") or "")
    if not p_skey:
        raise ApiError("QQ 授权会话不完整，请刷新二维码重试")

    authorize_form = {
        "response_type": "code",
        "client_id": 100497308,
        "redirect_uri": "https://y.qq.com/portal/wx_redirect.html?login_type=1&surl=https://y.qq.com/",
        "scope": "get_user_info,get_app_friends",
        "state": "state",
        "switch": "",
        "from_ptlogin": 1,
        "src": 1,
        "update_auth": 1,
        "openapi": "1010_1030",
        "g_tk": gtk_token({"p_skey": p_skey}),
        "auth_time": int(time.time() * 1000),
        "ui": f"{random.getrandbits(128):032x}",
    }
    authorize_opener = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(jar), NoRedirectHandler()
    )
    authorize_request = urllib.request.Request(
        QQ_AUTHORIZE_URL,
        data=urllib.parse.urlencode(authorize_form).encode("utf-8"),
        headers={
            **HEADERS,
            "Referer": "https://graph.qq.com/oauth2.0/login_jump",
            "Content-Type": "application/x-www-form-urlencoded",
        },
    )
    location = ""
    try:
        with authorize_opener.open(authorize_request, timeout=12) as response:
            location = str(response.headers.get("Location") or response.geturl())
    except urllib.error.HTTPError as error:
        if 300 <= error.code < 400:
            location = str(error.headers.get("Location") or "")
        else:
            raise ApiError(f"QQ 音乐授权失败: HTTP {error.code}") from error
    except (OSError, urllib.error.URLError) as error:
        raise ApiError(f"QQ 音乐授权失败: {error}") from error

    code = str(
        (urllib.parse.parse_qs(urllib.parse.urlparse(location).query).get("code") or [""])[0]
    )
    if not code:
        raise ApiError("QQ 音乐授权未返回登录凭证，请刷新二维码重试")

    login_payload = {
        "comm": {"uin": 0, "format": "json", "ct": 24, "cv": 0, "tmeLoginType": 2},
        "req_0": {
            "module": "QQConnectLogin.LoginServer",
            "method": "QQLogin",
            "param": {"code": code},
        },
    }
    login_response = request_json(MUSICU_URL, data=login_payload, timeout=15)
    credentials = login_credentials(
        login_response, "QQ 音乐账号授权失败，请刷新二维码重试"
    )

    jar_cookies["login_type"] = "1"
    return session_from_credentials(
        credentials,
        "qq",
        nickname=nickname,
        base_cookies=jar_cookies,
    )


def login_credentials(response: dict[str, Any], message: str) -> dict[str, Any]:
    result = response.get("req_0", {})
    if not isinstance(result, dict):
        raise ApiError(message)
    try:
        request_code = int(result.get("code") or 0)
    except (TypeError, ValueError):
        request_code = -1
    if request_code != 0:
        detail = clean_text(result.get("msg") or result.get("message"))
        suffix = f" (错误码 {request_code})" if request_code > 0 else ""
        raise ApiError(detail or f"{message}{suffix}")
    credentials = result.get("data")
    if not isinstance(credentials, dict):
        raise ApiError(message)

    for _ in range(2):
        if credentials.get("musicid") or credentials.get("str_musicid"):
            return credentials
        nested = credentials.get("data")
        try:
            business_code = int(credentials.get("code") or 0)
        except (TypeError, ValueError):
            business_code = -1
        if business_code != 0:
            detail = clean_text(
                credentials.get("msg") or credentials.get("message")
                or (nested.get("msg") if isinstance(nested, dict) else "")
                or (nested.get("message") if isinstance(nested, dict) else "")
            )
            suffix = f" (错误码 {business_code})" if business_code > 0 else ""
            raise ApiError(detail or f"{message}{suffix}")
        if not isinstance(nested, dict):
            break
        credentials = nested
    return credentials


def credentials_from_session(session: dict[str, Any]) -> dict[str, Any]:
    stored = session.get("credential")
    credentials = dict(stored) if isinstance(stored, dict) else {}
    cookies = session.get("cookies") if isinstance(session.get("cookies"), dict) else {}

    music_id = str(
        credentials.get("musicid")
        or credentials.get("str_musicid")
        or session.get("uin")
        or cookies.get("qqmusic_uin")
        or ""
    ).lstrip("o")
    music_key = str(
        credentials.get("musickey")
        or cookies.get("qqmusic_key")
        or cookies.get("qm_keyst")
        or ""
    )
    credentials.update(
        {
            "musicid": music_id,
            "str_musicid": str(credentials.get("str_musicid") or music_id),
            "musickey": music_key,
        }
    )

    cookie_fields = {
        "openid": ("psrf_qqopenid", "openid"),
        "access_token": ("psrf_qqaccess_token", "access_token"),
        "refresh_token": ("psrf_qqrefresh_token", "refresh_token"),
        "unionid": ("psrf_qqunionid", "unionid"),
        "refresh_key": ("refresh_key",),
        "encryptUin": ("euin",),
    }
    for field, names in cookie_fields.items():
        if credentials.get(field):
            continue
        value = next((cookies.get(name) for name in names if cookies.get(name)), "")
        if value:
            credentials[field] = str(value)

    login_type = str(credentials.get("loginType") or "")
    if login_type not in {"1", "2"}:
        browser_type = str(cookies.get("login_type") or "")
        if browser_type in {"1", "2"}:
            login_type = "2" if browser_type == "1" else "1"
        elif session.get("accountType") == "wechat":
            login_type = "1"
        elif session.get("accountType") == "qq":
            login_type = "2"
        else:
            login_type = "1" if music_key.startswith("W_X") else "2"
    credentials["loginType"] = int(login_type)
    return credentials


def validate_login_session(session: dict[str, Any]) -> bool:
    credentials = credentials_from_session(session)
    music_id = str(credentials.get("musicid") or "")
    music_key = str(credentials.get("musickey") or "")
    if not music_id.isdigit() or not music_key:
        return False

    comm = request_comm(session)
    comm.update(
        {
            "tmeLoginType": int(credentials.get("loginType") or 0),
            "authst": music_key,
        }
    )
    response = request_json(
        MUSICU_URL,
        data={
            "comm": comm,
            "req_0": {
                "module": "music.UserInfo.userInfoServer",
                "method": "GetLoginUserInfo",
                "param": {},
            },
        },
        cookies=session.get("cookies", {}),
        timeout=15,
    )
    result = response.get("req_0")
    if not isinstance(result, dict) or int(result.get("code") or 0) != 0:
        return False
    data = result.get("data")
    if not isinstance(data, dict) or int(data.get("code") or 0) != 0:
        return False
    info = data.get("info")
    return isinstance(info, dict) and int(info.get("retCode") or 0) == 0


def refresh_login_session(session: dict[str, Any]) -> dict[str, Any]:
    credentials = credentials_from_session(session)
    music_id = str(credentials.get("musicid") or "")
    music_key = str(credentials.get("musickey") or "")
    login_type = int(credentials.get("loginType") or 0)
    if not music_id.isdigit() or not music_key or login_type not in {1, 2}:
        raise ApiError("QQ 音乐登录凭证无法刷新，请重新登录")

    if login_type == 1:
        param = {
            "openid": credentials.get("openid"),
            "refresh_token": credentials.get("refresh_token"),
            "str_musicid": credentials.get("str_musicid") or music_id,
            "musickey": music_key,
            "unionid": credentials.get("unionid"),
            "refresh_key": credentials.get("refresh_key"),
            "loginMode": 2,
        }
    else:
        param = {
            "openid": credentials.get("openid"),
            "access_token": credentials.get("access_token"),
            "refresh_token": credentials.get("refresh_token"),
            "expired_in": credentials.get("expired_in")
                or credentials.get("expired_at"),
            "musicid": int(music_id),
            "musickey": music_key,
            "refresh_key": credentials.get("refresh_key"),
            "loginMode": 2,
        }
    param = {key: value for key, value in param.items() if value not in (None, "")}

    comm = request_comm(session)
    comm.update({"tmeLoginType": login_type, "authst": music_key})
    response = request_json(
        MUSICU_URL,
        data={
            "comm": comm,
            "req_0": {
                "module": "music.login.LoginServer",
                "method": "Login",
                "param": param,
            },
        },
        cookies=session.get("cookies", {}),
        timeout=15,
    )
    refreshed = login_credentials(response, "QQ 音乐登录续期失败，请重新登录")
    merged = dict(credentials)
    merged.update({key: value for key, value in refreshed.items() if value not in (None, "")})
    account_type = str(session.get("accountType") or "")
    if account_type not in {"qq", "wechat"}:
        account_type = "wechat" if login_type == 1 else "qq"
    result = session_from_credentials(
        merged,
        account_type,
        nickname=str(session.get("nickname") or ""),
        base_cookies=session.get("cookies", {}),
    )
    result["refreshedAt"] = int(time.time())
    write_private_json(SESSION_PATH, result)
    return result


def ensure_valid_session(force: bool = False) -> tuple[dict[str, Any], str]:
    session = load_session()
    if not session:
        return {}, "missing"

    now = int(time.time())
    if not force and now - int(session.get("validatedAt") or 0) < SESSION_VALIDATION_TTL:
        return session, "active"

    try:
        valid = validate_login_session(session)
    except ApiError:
        return session, "offline"

    if valid:
        session["credential"] = credentials_from_session(session)
        session["validatedAt"] = now
        write_private_json(SESSION_PATH, session)
        return session, "active"

    try:
        refreshed = refresh_login_session(session)
    except ApiError as error:
        if "服务请求失败" in str(error):
            return session, "offline"
        return {}, "expired"
    return refreshed, "active"


def session_status_payload() -> dict[str, Any]:
    session, state = ensure_valid_session(force=True)
    payload = session_public_payload(session)
    payload["sessionState"] = state
    return payload


def start_wechat_qr_login() -> dict[str, Any]:
    ensure_state_dir()
    page, _ = request_bytes(
        WECHAT_QR_URL,
        params={
            "appid": WECHAT_APP_ID,
            "redirect_uri": "https://y.qq.com/portal/wx_redirect.html?login_type=2&surl=https://y.qq.com/",
            "response_type": "code",
            "scope": "snsapi_login",
            "state": "STATE",
            "href": "https://y.qq.com/mediastyle/music_v17/src/css/popup_wechat.css#wechat_redirect",
        },
    )
    match = WECHAT_UUID_RE.search(page.decode("utf-8", errors="replace"))
    if not match:
        raise ApiError("微信登录服务没有返回二维码会话")
    uuid = match.group(1)
    image, _ = request_bytes(
        WECHAT_QR_IMAGE_URL.format(urllib.parse.quote(uuid)),
        extra_headers={"Referer": WECHAT_QR_URL},
    )
    if not image:
        raise ApiError("微信登录二维码为空，请稍后重试")

    QR_IMAGE_PATH.write_bytes(image)
    os.chmod(QR_IMAGE_PATH, 0o600)
    write_private_json(
        QR_STATE_PATH,
        {"type": "wechat", "createdAt": int(time.time()), "uuid": uuid},
    )
    return {
        "ok": True,
        "type": "wechat",
        "state": "waiting",
        "message": "等待微信扫码",
        "qrPath": str(QR_IMAGE_PATH),
    }


def poll_wechat_qr_login(state: dict[str, Any]) -> dict[str, Any]:
    uuid = str(state.get("uuid") or "")
    if not uuid or int(time.time()) - int(state.get("createdAt") or 0) > 300:
        return {"ok": True, "state": "expired", "message": "二维码已失效"}
    payload, _ = request_bytes(
        WECHAT_QR_POLL_URL,
        params={"uuid": uuid, "_": int(time.time() * 1000)},
        extra_headers={"Referer": "https://open.weixin.qq.com/"},
        timeout=38,
    )
    match = WECHAT_STATUS_RE.search(payload.decode("utf-8", errors="replace"))
    if not match:
        raise ApiError("微信登录状态响应无法解析")

    code, oauth_code = match.groups()
    if code == "408":
        return {"ok": True, "state": "waiting", "message": "等待微信扫码"}
    if code == "404":
        return {"ok": True, "state": "scanned", "message": "已扫码，等待确认"}
    if code in {"402", "403"}:
        return {
            "ok": True,
            "state": "expired",
            "message": "二维码已过期" if code == "402" else "已取消登录",
        }
    if code != "405" or not oauth_code:
        return {"ok": True, "state": "error", "message": "微信登录失败，请刷新重试"}

    response = request_json(
        MUSICU_URL,
        data={
            "comm": {"tmeLoginType": 1, "ct": 24, "cv": 0},
            "req_0": {
                "module": "music.login.LoginServer",
                "method": "Login",
                "param": {"code": oauth_code, "strAppid": WECHAT_APP_ID},
            },
        },
        timeout=15,
    )
    session = session_from_credentials(
        login_credentials(response, "微信账号授权失败，请刷新二维码重试"),
        "wechat",
    )
    write_private_json(SESSION_PATH, session)
    QR_STATE_PATH.unlink(missing_ok=True)
    QR_IMAGE_PATH.unlink(missing_ok=True)
    result = session_public_payload(session)
    result.update({"state": "success", "message": "登录成功", "type": "wechat"})
    return result


def start_qr_login(login_type: str) -> dict[str, Any]:
    QR_STATE_PATH.unlink(missing_ok=True)
    QR_IMAGE_PATH.unlink(missing_ok=True)
    if login_type == "wechat":
        return start_wechat_qr_login()
    return start_qq_qr_login()


def poll_qr_login(wait_seconds: float = 30.0) -> dict[str, Any]:
    state = read_private_json(QR_STATE_PATH)
    login_type = str(state.get("type") or "qq")
    if login_type == "wechat":
        return poll_wechat_qr_login(state)
    return poll_qq_qr_login(state)


def logout_session() -> dict[str, Any]:
    SESSION_PATH.unlink(missing_ok=True)
    QR_STATE_PATH.unlink(missing_ok=True)
    QR_IMAGE_PATH.unlink(missing_ok=True)
    AVATAR_IMAGE_PATH.unlink(missing_ok=True)
    AVATAR_SOURCE_PATH.unlink(missing_ok=True)
    return {"ok": True, "loggedIn": False}


def require_session() -> dict[str, Any]:
    session, state = ensure_valid_session()
    if not session:
        if state == "expired":
            raise ApiError("QQ 音乐登录已过期，请重新登录")
        raise ApiError("请先登录 QQ 音乐")
    return session


def extract_song_mids(value: Any) -> list[str]:
    result: list[str] = []

    def append(candidate: Any) -> None:
        text = str(candidate or "").strip()
        if re.fullmatch(r"[A-Za-z0-9]{10,20}", text) and text not in result:
            result.append(text)

    def visit(candidate: Any) -> None:
        if isinstance(candidate, dict):
            for key, item in candidate.items():
                if str(key).lower() in {"mid", "songmid", "song_mid"}:
                    append(item)
                else:
                    append(key)
                    visit(item)
        elif isinstance(candidate, list):
            for item in candidate:
                visit(item)
        elif isinstance(candidate, str):
            for item in candidate.split(","):
                append(item)

    visit(value)
    return result


def favorite_song_mids(session: dict[str, Any]) -> list[str]:
    cookies = session["cookies"]
    params = urllib.parse.urlencode(
        {
            "dirid": 201,
            "dirinfo": 1,
            "g_tk": gtk_token(cookies),
            "format": "json",
            "loginUin": session["uin"],
        }
    )
    payload = request_json(f"{FAVORITES_MAP_URL}?{params}", cookies=cookies)
    if int(payload.get("code") or 0) == 1000:
        raise ApiError("QQ 音乐登录已失效，请重新登录")
    mids = extract_song_mids(payload.get("mapmid"))
    if not mids:
        mids = extract_song_mids(payload.get("data", {}).get("mapmid"))
    return mids


def fetch_song_details(mids: list[str], authenticated: bool) -> list[dict[str, Any]]:
    if not mids:
        return []
    session = load_session() if authenticated else {}
    cookies = session.get("cookies", {})
    uin = str(session.get("uin") or "0")
    payload: dict[str, Any] = {
        "comm": {
            "uin": int(uin) if uin.isdigit() else 0,
            "format": "json",
            "ct": 24,
            "cv": 0,
        }
    }
    for index, mid in enumerate(mids):
        payload[f"song_{index}"] = {
            "module": "music.pf_song_detail_svr",
            "method": "get_song_detail_yqq",
            "param": {"song_mid": mid},
        }
    response = request_json(MUSICU_URL, data=payload, cookies=cookies, timeout=15)
    tracks: list[dict[str, Any]] = []
    for index in range(len(mids)):
        data = response.get(f"song_{index}", {}).get("data", {})
        track = data.get("track_info") if isinstance(data, dict) else None
        if isinstance(track, dict) and (track.get("mid") or track.get("songmid")):
            tracks.append(normalize_song(track, authenticated=authenticated))
    return tracks


def fetch_favorites(page: int, limit: int) -> dict[str, Any]:
    session = require_session()
    mids = favorite_song_mids(session)
    current_page = max(1, page)
    page_size = max(1, min(limit, 50))
    start = (current_page - 1) * page_size
    page_mids = mids[start : start + page_size]
    tracks = fetch_song_details(page_mids, authenticated=True)
    return {
        "ok": True,
        "page": current_page,
        "total": len(mids),
        "mids": mids,
        "tracks": tracks,
        "hasMore": start + len(page_mids) < len(mids),
    }


def history_track(raw: dict[str, Any]) -> dict[str, Any]:
    """Keep only playback metadata that is useful to the UI cache."""
    song_mid = str(raw.get("songMid") or "").strip()
    if not song_mid:
        raise ApiError("缺少歌曲标识")
    artists = raw.get("artists")
    artist_list = [clean_text(item) for item in artists] if isinstance(artists, list) else []
    artist_list = [item for item in artist_list if item]
    artist = first_text(raw.get("artist"), " / ".join(artist_list)) or "未知歌手"
    try:
        duration = max(0, int(raw.get("duration") or 0))
    except (TypeError, ValueError):
        duration = 0
    return {
        "id": str(raw.get("id") or song_mid),
        "songMid": song_mid,
        "mediaMid": str(raw.get("mediaMid") or song_mid),
        "title": first_text(raw.get("title")) or "未知歌曲",
        "artists": artist_list or artist.split(" / "),
        "artist": artist,
        "album": first_text(raw.get("album")),
        "albumMid": str(raw.get("albumMid") or ""),
        "duration": duration,
        "coverUrl": first_text(raw.get("coverUrl")),
        "vip": bool(raw.get("vip")),
        "playable": raw.get("playable") is not False,
    }


def recent_history_tracks() -> list[dict[str, Any]]:
    payload = read_private_json(PLAY_HISTORY_PATH)
    tracks = payload.get("tracks")
    if not isinstance(tracks, list):
        return []
    recent: list[dict[str, Any]] = []
    seen: set[str] = set()
    for item in tracks:
        if not isinstance(item, dict):
            continue
        try:
            track = history_track(item)
        except (ApiError, TypeError, ValueError):
            continue
        if track["songMid"] in seen:
            continue
        seen.add(track["songMid"])
        recent.append(track)
        if len(recent) >= 100:
            break
    return recent


def record_history_track(raw_track: str) -> dict[str, Any]:
    try:
        raw = json.loads(raw_track)
    except (TypeError, ValueError, json.JSONDecodeError) as error:
        raise ApiError("最近播放数据无效") from error
    if not isinstance(raw, dict):
        raise ApiError("最近播放数据无效")

    track = history_track(raw)
    recent = [item for item in recent_history_tracks() if item["songMid"] != track["songMid"]]
    recent.insert(0, track)
    write_private_json(PLAY_HISTORY_PATH, {"tracks": recent[:100]})
    return {"ok": True, "recentTracks": recent[:100]}


def playback_state(raw: dict[str, Any]) -> dict[str, Any]:
    """Validate the state needed to restore a paused playback session."""
    current_raw = raw.get("currentTrack")
    if not isinstance(current_raw, dict):
        raise ApiError("播放状态缺少歌曲")
    current_track = history_track(current_raw)

    queue: list[dict[str, Any]] = []
    raw_queue = raw.get("queue")
    if isinstance(raw_queue, list):
        for item in raw_queue[:100]:
            if not isinstance(item, dict):
                continue
            try:
                queue.append(history_track(item))
            except (ApiError, TypeError, ValueError):
                continue

    try:
        current_index = int(raw.get("currentIndex", -1))
    except (TypeError, ValueError):
        current_index = -1
    if not (0 <= current_index < len(queue)
            and queue[current_index]["songMid"] == current_track["songMid"]):
        current_index = next(
            (index for index, track in enumerate(queue)
             if track["songMid"] == current_track["songMid"]),
            -1,
        )
    if current_index < 0:
        queue.insert(0, current_track)
        current_index = 0

    try:
        position = max(0.0, float(raw.get("position", 0)))
    except (TypeError, ValueError):
        position = 0.0
    duration = float(current_track.get("duration") or 0)
    if duration > 0:
        position = min(position, duration)

    try:
        loop_state = int(raw.get("loopState", 0))
    except (TypeError, ValueError):
        loop_state = 0
    return {
        "currentTrack": current_track,
        "queue": queue,
        "currentIndex": current_index,
        "position": position,
        # Playback is intentionally never resumed automatically in a new UI session.
        "wasPlaying": False,
        "shuffle": bool(raw.get("shuffle")),
        "loopState": min(2, max(0, loop_state)),
    }


def load_playback_state() -> dict[str, Any]:
    raw = read_private_json(PLAYBACK_STATE_PATH)
    state = raw.get("state")
    if not isinstance(state, dict):
        return {"ok": True, "state": None}
    try:
        return {"ok": True, "state": playback_state(state)}
    except (ApiError, TypeError, ValueError):
        return {"ok": True, "state": None}


def save_playback_state(raw_state: str) -> dict[str, Any]:
    try:
        raw = json.loads(raw_state)
    except (TypeError, ValueError, json.JSONDecodeError) as error:
        raise ApiError("播放状态数据无效") from error
    if not isinstance(raw, dict):
        raise ApiError("播放状态数据无效")

    state = playback_state(raw)
    write_private_json(PLAYBACK_STATE_PATH, {"state": state})
    return {"ok": True}


def playlist_items(result: dict[str, Any], key: str) -> list[dict[str, Any]]:
    data = result.get("data") if isinstance(result.get("data"), dict) else result
    raw_items = data.get(key) if isinstance(data, dict) else []
    if not isinstance(raw_items, list):
        return []
    playlists: list[dict[str, Any]] = []
    for item in raw_items:
        if not isinstance(item, dict):
            continue
        playlist = normalize_playlist(item)
        if playlist.get("id"):
            playlists.append(playlist)
    return playlists


def fetch_library() -> dict[str, Any]:
    """Load local playback history plus the signed-in account's songlists."""
    recent = recent_history_tracks()
    session, state = ensure_valid_session()
    if not session:
        return {
            "ok": True,
            "recentTracks": recent,
            "collectedPlaylists": [],
            "createdPlaylists": [],
            "requiresLogin": True,
            "sessionState": state,
        }

    credentials = credentials_from_session(session)
    encrypted_uin = str(
        credentials.get("encryptUin") or credentials.get("euin") or session.get("uin") or "0"
    )
    response = request_json(
        MUSICU_URL,
        data={
            "comm": request_comm(session),
            "created": {
                "module": "music.musicasset.PlaylistBaseRead",
                "method": "GetPlaylistByUin",
                "param": {"uin": str(session.get("uin") or "0")},
            },
            "collected": {
                "module": "music.musicasset.PlaylistFavRead",
                "method": "CgiGetPlaylistFavInfo",
                "param": {"uin": encrypted_uin, "offset": 0, "size": 100},
            },
        },
        cookies=session.get("cookies", {}),
        timeout=20,
    )
    created_result = response.get("created", {})
    collected_result = response.get("collected", {})
    if not isinstance(created_result, dict) or int(created_result.get("code") or 0) != 0:
        raise ApiError(clean_text(created_result.get("msg")) or "自建歌单加载失败")
    if not isinstance(collected_result, dict) or int(collected_result.get("code") or 0) != 0:
        raise ApiError(clean_text(collected_result.get("msg")) or "收藏歌单加载失败")
    return {
        "ok": True,
        "recentTracks": recent,
        "collectedPlaylists": playlist_items(collected_result, "v_list"),
        "createdPlaylists": playlist_items(created_result, "v_playlist"),
        "requiresLogin": False,
        "sessionState": state,
    }


def resolve_song_info(session: dict[str, Any], mids: list[str]) -> dict[str, tuple[int, int]]:
    """把 songMid 换成 (数字 songId, 歌曲 type)。收藏写接口两样都要。"""
    wanted = [str(mid) for mid in mids if mid]
    if not wanted:
        return {}
    payload = request_json(
        MUSICU_URL,
        data={
            "comm": request_comm(session),
            "req": {
                "module": "music.trackInfo.UniformRuleCtrl",
                "method": "CgiGetTrackInfo",
                "param": {"mids": wanted, "types": [0] * len(wanted)},
            },
        },
        cookies=session.get("cookies", {}),
        timeout=15,
    )
    result = payload.get("req") if isinstance(payload.get("req"), dict) else {}
    data = result.get("data") if isinstance(result.get("data"), dict) else {}
    tracks = data.get("tracks") if isinstance(data.get("tracks"), list) else []
    resolved: dict[str, tuple[int, int]] = {}
    for track in tracks:
        if not isinstance(track, dict):
            continue
        mid = str(track.get("mid") or "")
        identifier = track.get("id")
        song_type = track.get("type")
        if mid and isinstance(identifier, int):
            resolved[mid] = (identifier, song_type if isinstance(song_type, int) else 0)
    return resolved


def resolve_song_ids(session: dict[str, Any], mids: list[str]) -> dict[str, int]:
    """把 songMid 换成数字 songId。"""
    return {mid: info[0] for mid, info in resolve_song_info(session, mids).items()}


def fetch_favorite_state(mids: list[str]) -> dict[str, Any]:
    """批量查这些歌是否已在「我喜欢」里。"""
    wanted = [str(mid) for mid in mids if mid]
    if not wanted:
        raise ApiError("缺少歌曲标识")
    session, _ = ensure_valid_session()
    if not session:
        raise ApiError("请先登录 QQ 音乐")
    payload = request_json(
        MUSICU_URL,
        data={
            "comm": request_comm(session),
            "req": {
                "module": "music.musicasset.SongFavRead",
                "method": "IsSongFanByMid",
                "param": {"v_songMid": wanted},
            },
        },
        cookies=session.get("cookies", {}),
        timeout=15,
    )
    result = payload.get("req") if isinstance(payload.get("req"), dict) else {}
    if int(result.get("code") or 0) != 0:
        raise ApiError(clean_text(result.get("msg")) or "收藏状态查询失败")
    data = result.get("data") if isinstance(result.get("data"), dict) else {}
    fans = data.get("m_fan") if isinstance(data.get("m_fan"), dict) else {}
    return {
        "ok": True,
        "fans": {mid: bool(fans.get(mid)) for mid in wanted},
    }


def write_playlist_songs(action: str, dir_id: int, song_mid: str) -> dict[str, Any]:
    """往自己的歌单（按 dirId）里加 / 删一首歌。「我喜欢」就是 dirId=201。

    music.musicasset.PlaylistDetailWrite 的 AddSonglist / DelSonglist，照网页端：
    经 musics.fcg 带签名发，v_songInfo 是 {songType: 歌曲自己的 type, songId: 数字 id}。
    songType 不能写死（见 FAVORITE_DIR_ID 上方的说明），所以总要先查一次曲目信息。
    """
    if action not in ("add", "remove"):
        raise ApiError("未知的歌单操作")
    session = require_session()
    if not song_mid:
        raise ApiError("缺少歌曲标识")
    info = resolve_song_info(session, [song_mid]).get(str(song_mid))
    if info is None:
        raise ApiError("查不到这首歌的曲目信息，暂时无法操作")
    numeric_id, song_type = info

    payload = request_signed(session, {
        "req": {
            "module": "music.musicasset.PlaylistDetailWrite",
            "method": "AddSonglist" if action == "add" else "DelSonglist",
            "param": {
                "dirId": int(dir_id),
                "v_songInfo": [{"songType": song_type, "songId": numeric_id}],
            },
        },
    })
    result = payload.get("req") if isinstance(payload.get("req"), dict) else {}
    code = int(result.get("code") or 0)
    if code == 1000:
        raise ApiError("QQ 音乐登录已失效，请重新登录")
    if code != 0:
        raise ApiError(clean_text(result.get("msg")) or f"更新歌单失败（code {code}）")
    data = result.get("data") if isinstance(result.get("data"), dict) else {}
    ret = int(data.get("retCode") or 0)
    if ret != 0:
        raise ApiError(clean_text(data.get("msg")) or f"更新歌单失败（retCode {ret}）")
    return {"ok": True, "songMid": song_mid, "songId": numeric_id, "dirId": int(dir_id),
            "added": action == "add"}


def mutate_favorite(action: str, song_mid: str, song_id: str) -> dict[str, Any]:
    """加/删「我喜欢」（dirId 201 的歌单）。song_id 保留只为兼容旧调用，写接口按曲目信息里的 id。"""
    result = write_playlist_songs(action, FAVORITE_DIR_ID, song_mid)
    return {
        "ok": True,
        "songMid": song_mid,
        "songId": result["songId"],
        "favorite": action == "add",
    }


def create_playlist(name: str, description: str = "") -> dict[str, Any]:
    """新建一个自己的歌单。参数形状照网页端的 AddPlaylist（common chunk）。"""
    title = str(name or "").strip()
    if not title:
        raise ApiError("歌单名不能为空")
    session = require_session()
    payload = request_signed(session, {
        "req": {
            "module": "music.musicasset.PlaylistBaseWrite",
            "method": "AddPlaylist",
            "param": {"dirName": title, "dirDesc": str(description or ""),
                      "dirPicUrl": "", "taglist": []},
        },
    })
    result = payload.get("req") if isinstance(payload.get("req"), dict) else {}
    code = int(result.get("code") or 0)
    if code == 1000:
        raise ApiError("QQ 音乐登录已失效，请重新登录")
    if code == 4:  # 网页端对 4 的提示就是这句
        raise ApiError("已经有同名的歌单了，换个名字")
    if code != 0:
        raise ApiError(clean_text(result.get("msg")) or f"新建歌单失败（code {code}）")
    data = result.get("data") if isinstance(result.get("data"), dict) else {}
    info = data.get("result") if isinstance(data.get("result"), dict) else {}
    return {
        "ok": True,
        "playlist": {
            "id": str(info.get("tid") or ""),
            "kind": "playlist",
            "dirId": str(info.get("dirId") or ""),
            "title": clean_text(info.get("dirName")) or title,
            "subtitle": "",
            "coverUrl": str(info.get("dirPicUrl") or ""),
            "songCount": 0,
            "playCount": 0,
            "creator": "",
        },
    }


def delete_playlist(dir_id: str) -> dict[str, Any]:
    """删除自己创建的歌单（按 dirId）。「我喜欢」（201）不允许删。"""
    raw = str(dir_id or "").strip()
    if not raw.isdigit():
        raise ApiError("歌单目录标识无效")
    if int(raw) == FAVORITE_DIR_ID:
        raise ApiError("「我喜欢」不能删除")
    session = require_session()
    payload = request_signed(session, {
        "req": {
            "module": "music.musicasset.PlaylistBaseWrite",
            "method": "DelPlaylist",
            "param": {"dirId": int(raw)},
        },
    })
    result = payload.get("req") if isinstance(payload.get("req"), dict) else {}
    code = int(result.get("code") or 0)
    if code == 1000:
        raise ApiError("QQ 音乐登录已失效，请重新登录")
    if code != 0:
        raise ApiError(clean_text(result.get("msg")) or f"删除歌单失败（code {code}）")
    return {"ok": True, "dirId": raw}


def post_comment(song_id: str, content: str, replied_comment_id: str = "") -> dict[str, Any]:
    """给歌曲发一条评论。"""
    identifier = str(song_id or "").strip()
    if not identifier.isdigit():
        raise ApiError("该歌曲没有可用的评论标识")
    text = str(content or "").strip()
    if not text:
        raise ApiError("评论内容不能为空")
    session, _ = ensure_valid_session()
    if not session:
        raise ApiError("请先登录 QQ 音乐")
    # 模块、方法、参数照网页端（common chunk 里的 AddComment 调用）原样：
    # 网页端经 musics.fcg 带签名发，BgCardId 没有评论背景卡时是 undefined，序列化时整个字段消失。
    param: dict[str, Any] = {
        "BizType": 1,
        "BizId": identifier,
        "Content": text,
        "RepliedCmId": str(replied_comment_id or ""),
    }
    payload = request_signed(session, {
        "req": {
            "module": "music.globalComment.CommentWriteServer",
            "method": "AddComment",
            "param": param,
        },
    })
    result = payload.get("req") if isinstance(payload.get("req"), dict) else {}
    code = int(result.get("code") or 0)
    # 20015 = 已受理但需要审核。官方前端把这种情况当成功（返回 null）。
    if code == 20015:
        return {"ok": True, "pending": True, "message": "评论已提交，等待审核"}
    if code == 1000:
        raise ApiError("QQ 音乐登录已失效，请重新登录")
    if code != 0:
        raise ApiError(clean_text(result.get("msg")) or "评论发送失败")
    data = result.get("data") if isinstance(result.get("data"), dict) else {}
    return {"ok": True, "pending": False, "comment": data}


# 评论操作类型，取自网页端的枚举（HOT=1, CANCEL_HOT=2, PRAISE=3, CANCEL_PRAISE=4, ...）。
COMMENT_PRAISE = 3
COMMENT_CANCEL_PRAISE = 4


def praise_comment(comment_id: str, praise: bool) -> dict[str, Any]:
    """给评论点赞 / 取消点赞。

    走网页端的 GlobalComment.GlobalCommentWriteServer / UpdateHotComment（经 musics.fcg 带签名）。
    踩过的坑：安卓端的 music.globalComment.CommentWrite / SetCommentPraise 用网页 comm 发，
    服务端回 req.code 40000，点赞数和 IsPraised 都不变。
    网页端判成功的条件是 data.code 和 data.subcode 都为 0，这里照搬。
    """
    identifier = str(comment_id or "").strip()
    if not identifier:
        raise ApiError("缺少评论 ID")
    session = require_session()
    payload = request_signed(session, {
        "req": {
            "module": "GlobalComment.GlobalCommentWriteServer",
            "method": "UpdateHotComment",
            "param": {
                "comment_id": identifier,
                "type": COMMENT_PRAISE if praise else COMMENT_CANCEL_PRAISE,
                "uin": str(session.get("uin") or ""),
            },
        },
    })
    result = payload.get("req") if isinstance(payload.get("req"), dict) else {}
    code = int(result.get("code") or 0)
    if code == 1000:
        raise ApiError("QQ 音乐登录已失效，请重新登录")
    if code != 0:
        raise ApiError(clean_text(result.get("msg")) or f"点赞失败（code {code}）")
    data = result.get("data") if isinstance(result.get("data"), dict) else {}
    sub_code = int(data.get("code") or 0)
    sub_sub = int(data.get("subcode") or 0)
    if sub_code != 0 or sub_sub != 0:
        message = clean_text(data.get("msg") or data.get("Msg"))
        raise ApiError(message or f"点赞失败（code {sub_code}/{sub_sub}）")
    return {"ok": True, "commentId": identifier, "praised": praise}


def delete_comment(comment_id: str) -> dict[str, Any]:
    """删除自己发过的评论。"""
    identifier = str(comment_id or "").strip()
    if not identifier:
        raise ApiError("缺少评论 ID")
    session, _ = ensure_valid_session()
    if not session:
        raise ApiError("请先登录 QQ 音乐")
    payload = request_signed(session, {
        "req": {
            "module": "music.globalComment.CommentWriteServer",
            "method": "DelComment",
            "param": {"CommentId": identifier},
        },
    })
    result = payload.get("req") if isinstance(payload.get("req"), dict) else {}
    code = int(result.get("code") or 0)
    if code == 1000:
        raise ApiError("QQ 音乐登录已失效，请重新登录")
    if code != 0:
        raise ApiError(clean_text(result.get("msg")) or "评论删除失败")
    return {"ok": True, "commentId": identifier}


# ---------- musics.fcg 签名 ----------
#
# 算法来自 y.qq.com 网页端（zzc 版本），参考 jixunmoe/qmweb-sign（MIT）的分析：
# 对**请求体原文**做 SHA-1 取大写十六进制，按固定下标抽两段字符，
# 中间 20 字节与固定表异或后 base64（去掉 / + =），拼成 "zzc" + 前段 + base64 + 后段，再整体小写。
# 签的是发出去的那串字节本身，所以请求体必须先序列化好、签名和发送用同一份。
# 网页端还支持 encoding=ag-1 的请求体加密，实测不加密也能通，这里不做。

_ZZC_PART1 = [23, 14, 6, 36, 16, 7, 19]  # 原表里还有个 40，越界在 JS 里取到 undefined → 空串，等于不存在
_ZZC_PART2 = [16, 1, 32, 12, 19, 27, 8, 5]
_ZZC_SCRAMBLE = [89, 39, 179, 150, 218, 82, 58, 252, 177, 52,
                 186, 123, 120, 64, 242, 133, 143, 161, 121, 179]


def zzc_sign(body: str) -> str:
    digest = hashlib.sha1(body.encode("utf-8")).hexdigest().upper()
    part1 = "".join(digest[i] for i in _ZZC_PART1)
    part2 = "".join(digest[i] for i in _ZZC_PART2)
    scrambled = bytes(
        value ^ int(digest[i * 2 : i * 2 + 2], 16) for i, value in enumerate(_ZZC_SCRAMBLE)
    )
    middle = base64.b64encode(scrambled).decode("ascii")
    for char in "/+=":
        middle = middle.replace(char, "")
    return f"zzc{part1}{middle}{part2}".lower()


def web_comm(session: dict[str, Any]) -> dict[str, Any]:
    """网页端发 musics.fcg 时带的 comm，写操作按它来。"""
    cookies = session.get("cookies", {})
    uin = str(session.get("uin") or "0")
    token = gtk_token(cookies)
    return {
        "cv": 4747474,
        "ct": 24,
        "format": "json",
        "inCharset": "utf-8",
        "outCharset": "utf-8",
        "notice": 0,
        "platform": "yqq.json",
        "needNewCode": 1,
        "uin": int(uin) if uin.isdigit() else 0,
        "g_tk_new_20200303": token,
        "g_tk": token,
    }


def request_signed(
    session: dict[str, Any], requests: dict[str, Any], timeout: float = 20.0,
) -> dict[str, Any]:
    """经 musics.fcg 发一组模块请求（自动带签名和网页端 comm）。"""
    payload = {"comm": web_comm(session), **requests}
    body = json.dumps(payload, ensure_ascii=False, separators=(",", ":"))
    query = urllib.parse.urlencode(
        {"_": str(int(time.time() * 1000)), "sign": zzc_sign(body)}
    )
    headers = dict(HEADERS)
    headers["Origin"] = "https://y.qq.com"
    headers["Content-Type"] = "application/x-www-form-urlencoded"
    cookies = session.get("cookies", {})
    if cookies:
        headers["Cookie"] = cookie_header(cookies)
    request = urllib.request.Request(
        f"{MUSICS_URL}?{query}", data=body.encode("utf-8"), headers=headers,
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            raw = response.read()
    except (OSError, urllib.error.URLError) as error:
        raise ApiError(f"QQ 音乐服务请求失败: {error}") from error
    # 和 request_json 一样：服务端偶尔回 GBK，utf-8 解不开就退回 gbk，免得真错误信息被吞掉。
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError:
        text = raw.decode("gbk", errors="replace")
    try:
        result = json.loads(text)
    except ValueError as error:
        raise ApiError("QQ 音乐服务返回了无法解析的内容") from error
    top = int(result.get("code") or 0) if isinstance(result, dict) else -1
    if top == 2000:
        raise ApiError("请求签名被拒绝（code 2000），签名算法可能又更新了")
    if top != 0:
        raise ApiError(f"QQ 音乐服务返回错误（code {top}）")
    return result


def request_comm(session: dict[str, Any] | None = None) -> dict[str, Any]:
    current = session or {}
    uin = str(current.get("uin") or "0")
    return {
        "ct": 24,
        "cv": 4747474,
        "format": "json",
        "uin": int(uin) if uin.isdigit() else 0,
    }


def recommendation_tracks(
    response: dict[str, Any], key: str, *, radar: bool = False,
) -> list[dict[str, Any]]:
    result = response.get(key)
    if not isinstance(result, dict) or int(result.get("code") or 0) != 0:
        return []
    data = result.get("data")
    if not isinstance(data, dict):
        return []
    raw_tracks = data.get("VecSongs") if radar else data.get("tracks")
    tracks = []
    for item in raw_tracks if isinstance(raw_tracks, list) else []:
        source = item.get("Track") if radar and isinstance(item, dict) else item
        if isinstance(source, dict) and (source.get("mid") or source.get("songmid")):
            tracks.append(normalize_song(source, authenticated=True))
    return tracks


def radar_tracks(
    session: dict[str, Any], favorite_tracks: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    favorite_ids = [
        int(track["id"])
        for track in favorite_tracks
        if str(track.get("id") or "").isdigit()
    ]
    response = request_json(
        MUSICU_URL,
        data={
            "comm": request_comm(session),
            "radar": {
                "module": "music.recommend.TrackRelationServer",
                "method": "GetRadarSong",
                "param": {
                    "Page": 1,
                    "ReqType": 0,
                    "FavSongs": favorite_ids,
                    "EntranceSongs": [],
                },
            },
        },
        cookies=session.get("cookies", {}),
        timeout=20,
    )
    return recommendation_tracks(response, "radar", radar=True)


def favorite_seed_tracks(session: dict[str, Any], limit: int = 8) -> list[dict[str, Any]]:
    mids = favorite_song_mids(session)
    return fetch_song_details(mids[:max(1, min(limit, 20))], authenticated=True)


def fetch_favorite_radar() -> dict[str, Any]:
    session = require_session()
    seeds = favorite_seed_tracks(session)
    if not seeds:
        raise ApiError("先添加红心歌曲，再开始音乐探索")
    return {
        "ok": True,
        "seedTracks": seeds,
        "tracks": radar_tracks(session, seeds),
    }


def fetch_comments(
    song_id: str, page: int, limit: int, last_comment_seq_no: str = "", sort: str = "hot",
) -> dict[str, Any]:
    """拉评论。sort=hot 热评（按页码翻），sort=new 最新（按游标翻）。

    最新评论的翻页只认游标：LastCommentSeqNo 填上一页最后一条的 seqNo，
    不带游标时 PageNum 不起作用、永远回第一页（实测）。热评按页码翻即可。
    """
    identifier = str(song_id or "").strip()
    if not identifier.isdigit():
        raise ApiError("该歌曲没有可用的评论标识")
    if sort not in ("hot", "new"):
        raise ApiError("未知的评论排序")
    session, _ = ensure_valid_session()
    current_page = max(1, page)
    page_size = max(1, min(limit, 30))
    if sort == "new":
        request = {
            "module": "music.globalComment.CommentRead",
            "method": "GetNewCommentList",
            "param": {
                "BizType": 1,
                "BizId": identifier,
                "LastCommentSeqNo": str(last_comment_seq_no or ""),
                "PageSize": page_size,
                "PageNum": current_page - 1,
                "FromCommentId": "",
                "WithHot": 0,
                "PicEnable": 1,
                "LastTotal": 0,
                "LastTotalVer": "0",
            },
        }
    else:
        request = {
            "module": "music.globalComment.CommentRead",
            "method": "GetHotCommentList",
            "param": {
                "BizType": 1,
                "BizId": identifier,
                "LastCommentSeqNo": str(last_comment_seq_no or ""),
                "PageSize": page_size,
                "PageNum": current_page - 1,
                "HotType": 1,
                "WithAirborne": 0,
                "PicEnable": 1,
            },
        }
    response = request_json(
        MUSICU_URL,
        data={
            "comm": request_comm(session),
            "comments": request,
        },
        cookies=session.get("cookies", {}),
        timeout=20,
    )
    result = response.get("comments")
    if not isinstance(result, dict) or int(result.get("code") or 0) != 0:
        message = clean_text(result.get("msg")) if isinstance(result, dict) else ""
        raise ApiError(message or "歌曲评论加载失败")
    data = result.get("data")
    comment_list = data.get("CommentList") if isinstance(data, dict) else {}
    if not isinstance(comment_list, dict):
        comment_list = {}
    comments = []
    for item in comment_list.get("Comments", []):
        if not isinstance(item, dict):
            continue
        comments.append(
            {
                "id": str(item.get("CmId") or ""),
                "seqNo": str(item.get("SeqNo") or ""),
                "author": clean_text(item.get("Nick")) or "QQ 音乐用户",
                "avatarUrl": str(item.get("Avatar") or ""),
                "text": clean_text(item.get("Content")),
                "time": max(0, int(item.get("PubTime") or 0)),
                "likes": max(0, int(item.get("PraiseNum") or 0)),
                "replies": max(0, int(item.get("ReplyCnt") or 0)),
                "isPraised": bool(int(item.get("IsPraised") or 0)),
            }
        )
    return {
        "ok": True,
        "songId": identifier,
        "sort": sort,
        "page": current_page,
        "total": max(0, int(comment_list.get("Total") or 0)),
        "hasMore": bool(comment_list.get("HasMore")),
        "comments": comments,
    }


def find_daily_playlist(data: dict[str, Any]) -> dict[str, Any]:
    for shelf in data.get("v_shelf", []):
        if not isinstance(shelf, dict):
            continue
        for niche in shelf.get("v_niche", []):
            if not isinstance(niche, dict):
                continue
            for card in niche.get("v_card", []):
                if not isinstance(card, dict):
                    continue
                if clean_text(card.get("title")) == "每日30首" and card.get("id"):
                    return {
                        "id": str(card.get("id")),
                        "title": "每日30首",
                        "subtitle": "根据你的音乐偏好每日更新",
                        "coverUrl": first_text(card.get("cover")),
                        "songCount": 30,
                        "playCount": 0,
                        "creator": "QQ 音乐",
                    }
    return {}


def fetch_playlist(playlist_id: str, page: int, limit: int, dir_id: str = "") -> dict[str, Any]:
    identifier = str(playlist_id or "").strip()
    if not identifier.isdigit():
        raise ApiError("歌单标识无效")
    directory = str(dir_id or "").strip()
    if directory and not directory.isdigit():
        raise ApiError("歌单目录标识无效")
    session, _ = ensure_valid_session()
    current_page = max(1, page)
    page_size = max(1, min(limit, 50))
    response = request_json(
        MUSICU_URL,
        data={
            "comm": request_comm(session),
            "req_0": {
                "module": "music.srfDissInfo.DissInfo",
                "method": "CgiGetDiss",
                "param": {
                    "disstid": int(identifier),
                    "dirid": int(directory or 0),
                    "tag": 1,
                    "song_begin": page_size * (current_page - 1),
                    "song_num": page_size,
                    "userinfo": 1,
                    "orderlist": 1,
                    "onlysonglist": 0,
                },
            },
        },
        cookies=session.get("cookies", {}),
        timeout=20,
    )
    result = response.get("req_0", {})
    if int(result.get("code") or 0) != 0:
        raise ApiError(clean_text(result.get("msg")) or "歌单加载失败")
    data = result.get("data")
    if not isinstance(data, dict):
        raise ApiError("歌单响应格式无效")
    raw_tracks = data.get("songlist")
    if not isinstance(raw_tracks, list):
        raw_tracks = []
    tracks = [
        normalize_song(track, authenticated=bool(session))
        for track in raw_tracks
        if isinstance(track, dict) and (track.get("mid") or track.get("songmid"))
    ]
    total = max(len(tracks), int(data.get("total_song_num") or 0))
    info = normalize_playlist(data.get("dirinfo") or {})
    if not info.get("id"):
        info["id"] = identifier
    if not info.get("dirId") and directory:
        info["dirId"] = directory
    if info.get("songCount", 0) <= 0:
        info["songCount"] = total
    return {
        "ok": True,
        "playlist": info,
        "page": current_page,
        "total": total,
        "tracks": tracks,
        "hasMore": bool(data.get("hasmore"))
            or current_page * page_size < total,
    }


def discover_content() -> dict[str, Any]:
    session, _ = ensure_valid_session()
    payload: dict[str, Any] = {
        "comm": request_comm(session),
        "playlists": {
            "module": "music.playlist.PlaylistSquare",
            "method": "GetRecommendFeed",
            "param": {"From": 0, "Size": 12},
        },
    }
    if session:
        payload["feed"] = {
            "module": "music.recommend.RecommendFeed",
            "method": "get_recommend_feed",
            "param": {"direction": 0, "page": 1, "s_num": 0, "v_cache": []},
        }
        payload["guess"] = {
            "module": "music.radioProxy.MbTrackRadioSvr",
            "method": "get_radio_track",
            "param": {"id": 99, "num": 12, "from": 0, "scene": 0, "song_ids": []},
        }
        payload["radar"] = {
            "module": "music.recommend.TrackRelationServer",
            "method": "GetRadarSong",
            "param": {"Page": 1, "ReqType": 0, "FavSongs": [], "EntranceSongs": []},
        }
    response = request_json(
        MUSICU_URL,
        data=payload,
        cookies=session.get("cookies", {}),
        timeout=20,
    )

    playlist_result = response.get("playlists", {})
    playlist_data = playlist_result.get("data", {})
    if int(playlist_result.get("code") or 0) != 0 or not isinstance(playlist_data, dict):
        raise ApiError("推荐歌单加载失败")
    recommended = []
    for item in playlist_data.get("List", []):
        if isinstance(item, dict):
            normalized = normalize_playlist(item)
            if normalized.get("id"):
                recommended.append(normalized)

    daily_playlist: dict[str, Any] = {}
    daily_tracks: list[dict[str, Any]] = []
    guess_tracks: list[dict[str, Any]] = []
    radar_recommendations: list[dict[str, Any]] = []
    if session:
        feed_result = response.get("feed", {})
        feed_data = feed_result.get("data", {})
        if int(feed_result.get("code") or 0) == 0 and isinstance(feed_data, dict):
            daily_playlist = find_daily_playlist(feed_data)
        if daily_playlist.get("id"):
            daily_result = fetch_playlist(str(daily_playlist["id"]), 1, 30)
            daily_tracks = daily_result["tracks"]
            detail = daily_result.get("playlist") or {}
            if detail.get("coverUrl"):
                daily_playlist["coverUrl"] = detail["coverUrl"]
            daily_playlist["songCount"] = int(daily_result.get("total") or len(daily_tracks))
        guess_tracks = recommendation_tracks(response, "guess")
        radar_recommendations = recommendation_tracks(response, "radar", radar=True)

    return {
        "ok": True,
        "dailyPlaylist": daily_playlist,
        "dailyTracks": daily_tracks,
        "dailyRequiresLogin": not bool(session),
        "recommendedPlaylists": recommended,
        "guessTracks": guess_tracks,
        "radarTracks": radar_recommendations,
        "favoriteSeedTracks": [],
    }


def fetch_daily() -> dict[str, Any]:
    session, _ = ensure_valid_session()
    if not session:
        raise ApiError("登录后才能查看每日30首")
    result = discover_content()
    return {
        "ok": True,
        "playlist": result.get("dailyPlaylist", {}),
        "tracks": result.get("dailyTracks", []),
    }


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="QQ Music JSON helper")
    subparsers = parser.add_subparsers(dest="command", required=True)

    search = subparsers.add_parser("search")
    search.add_argument("query")
    search.add_argument("--page", type=int, default=1)
    search.add_argument("--limit", type=int, default=30)
    search.add_argument("--type", choices=tuple(SEARCH_TYPES), default="song",
                        help="搜哪一类：song / singer / album / songlist")

    album = subparsers.add_parser("album")
    album.add_argument("--album-mid", required=True)
    album.add_argument("--page", type=int, default=1)
    album.add_argument("--limit", type=int, default=50)

    resolve = subparsers.add_parser("resolve")
    resolve.add_argument("--song-mid", required=True)
    resolve.add_argument("--media-mid", default="")
    resolve.add_argument("--quality", choices=[key for key, _, _ in QUALITY_LADDER], default="")

    lyrics = subparsers.add_parser("lyrics")
    lyrics.add_argument("--song-mid", required=True)

    subparsers.add_parser("session-status")
    login_qr = subparsers.add_parser("login-qr")
    login_qr.add_argument("--type", choices=("qq", "wechat"), default="qq")

    login_poll = subparsers.add_parser("login-poll")
    login_poll.add_argument("--wait", type=float, default=30.0)
    subparsers.add_parser("logout")

    subparsers.add_parser("discover")
    subparsers.add_parser("daily")
    subparsers.add_parser("library")

    history = subparsers.add_parser("history")
    history.add_argument("action", choices=("record",))
    history.add_argument("--track", required=True)

    playback_state_parser = subparsers.add_parser("playback-state")
    playback_state_parser.add_argument("action", choices=("load", "save"))
    playback_state_parser.add_argument("--state", default="")

    playlist = subparsers.add_parser("playlist")
    playlist.add_argument("--id", required=True)
    playlist.add_argument("--dir-id", default="")
    playlist.add_argument("--page", type=int, default=1)
    playlist.add_argument("--limit", type=int, default=50)

    favorites = subparsers.add_parser("favorites")
    favorites.add_argument("--page", type=int, default=1)
    favorites.add_argument("--limit", type=int, default=50)

    subparsers.add_parser("favorite-radar")

    comments = subparsers.add_parser("comments")
    comments.add_argument("--song-id", required=True)
    comments.add_argument("--page", type=int, default=1)
    comments.add_argument("--limit", type=int, default=20)
    comments.add_argument("--cursor", default="")
    comments.add_argument("--sort", choices=("hot", "new"), default="hot")

    favorite = subparsers.add_parser("favorite")
    favorite.add_argument("action", choices=("add", "remove"))
    favorite.add_argument("--song-mid", default="")
    favorite.add_argument("--song-id", default="")

    favorite_check = subparsers.add_parser("favorite-check")
    favorite_check.add_argument("--song-mid", action="append", default=[])

    comment_add = subparsers.add_parser("comment-add")
    comment_add.add_argument("--song-id", required=True)
    comment_add.add_argument("--content", required=True)
    comment_add.add_argument("--reply-to", default="")

    comment_del = subparsers.add_parser("comment-del")
    comment_del.add_argument("--comment-id", required=True)

    playlist_create = subparsers.add_parser("playlist-create")
    playlist_create.add_argument("--name", required=True)
    playlist_create.add_argument("--desc", default="")

    playlist_delete = subparsers.add_parser("playlist-delete")
    playlist_delete.add_argument("--dir-id", required=True)

    playlist_song = subparsers.add_parser("playlist-song")
    playlist_song.add_argument("action", choices=("add", "remove"))
    playlist_song.add_argument("--dir-id", required=True, type=int)
    playlist_song.add_argument("--song-mid", required=True)

    comment_praise = subparsers.add_parser("comment-praise")
    comment_praise.add_argument("action", choices=("like", "unlike"))
    comment_praise.add_argument("--comment-id", required=True)

    return parser



def main() -> int:
    args = build_parser().parse_args()
    try:
        if args.command == "search":
            result = search_tracks(args.query, args.page, args.limit, args.type)
        elif args.command == "album":
            result = fetch_album(args.album_mid, args.page, args.limit)
        elif args.command == "resolve":
            result = resolve_track(args.song_mid, args.media_mid, args.quality)
        elif args.command == "lyrics":
            result = fetch_lyrics(args.song_mid)
        elif args.command == "session-status":
            result = session_status_payload()
        elif args.command == "login-qr":
            result = start_qr_login(args.type)
        elif args.command == "login-poll":
            result = poll_qr_login(max(1.0, min(args.wait, 60.0)))
        elif args.command == "logout":
            result = logout_session()
        elif args.command == "discover":
            result = discover_content()
        elif args.command == "daily":
            result = fetch_daily()
        elif args.command == "library":
            result = fetch_library()
        elif args.command == "history":
            result = record_history_track(args.track)
        elif args.command == "playback-state":
            result = load_playback_state() if args.action == "load" else save_playback_state(args.state)
        elif args.command == "playlist":
            result = fetch_playlist(args.id, args.page, args.limit, args.dir_id)
        elif args.command == "favorites":
            result = fetch_favorites(args.page, args.limit)
        elif args.command == "favorite-radar":
            result = fetch_favorite_radar()
        elif args.command == "comments":
            result = fetch_comments(args.song_id, args.page, args.limit, args.cursor, args.sort)
        elif args.command == "favorite":
            result = mutate_favorite(args.action, args.song_mid, args.song_id)
        elif args.command == "favorite-check":
            result = fetch_favorite_state(args.song_mid)
        elif args.command == "comment-add":
            result = post_comment(args.song_id, args.content, args.reply_to)
        elif args.command == "comment-del":
            result = delete_comment(args.comment_id)
        elif args.command == "playlist-create":
            result = create_playlist(args.name, args.desc)
        elif args.command == "playlist-delete":
            result = delete_playlist(args.dir_id)
        elif args.command == "playlist-song":
            result = write_playlist_songs(args.action, args.dir_id, args.song_mid)
        elif args.command == "comment-praise":
            result = praise_comment(args.comment_id, args.action == "like")
        else:
            raise ApiError(f"未知命令：{args.command}")
        print(json.dumps(result, ensure_ascii=False, separators=(",", ":")))
        return 0

    except ApiError as error:
        print(
            json.dumps(
                {"ok": False, "error": str(error)},
                ensure_ascii=False,
                separators=(",", ":"),
            )
        )
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
