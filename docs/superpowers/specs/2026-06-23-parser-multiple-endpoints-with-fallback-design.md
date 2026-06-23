# 解析器多端点 + 降级 设计稿

**日期**: 2026-06-23
**状态**: 待用户复核
**范围**: 单个子项目——`ParserService.Parse(shareURL)` 改用 `feed/profile` 为主、`parse_sph` 降级，新增统一的响应转换层
**前置依赖**: 无（仅依赖 `http://127.0.0.1:2022` 已在跑的上游服务）

## 背景与目标

`internal/service/parser.go::Parse(shareURL)` 当前只调用一个上游端点：

```
GET {api_base_url}/api/channels/shared_feed/profile?url=<shareURL>
```

该端点在 `wx_channels_download` 参考项目里有更通用的等价物：

```
GET {api_base_url}/api/channels/feed/profile?url=<shareURL>
```

`feed/profile` 在参考项目的 `internal/api/handler.go::handleFetchFeedProfile` 里同时支持 `oid/nid/url/eid` 四种入参，路由到同一套前端 WebSocket 调用，行为更稳定。当 shareURL 走不通时，`feed/profile` 服务端并不会自动 fallback。

参考项目额外提供一个独立的兜底端点：

```
GET {api_base_url}/api/channels/parse_sph?url=<shareURL>
```

`parse_sph` 走腾讯 yuanbao 上游（`parseShareUrl` + `getFeedInfo` 两步），专门用于 `weixin.qq.com/sph/...` 这类短链。它的响应 shape 跟 `feed/profile` 不一样：用的是 `authorInfo/feedInfo/sceneInfo` 嵌套，而 `feed/profile` 用的是 `object/objectDesc/media/contact` 嵌套。

**目标**：

1. `Parse(shareURL)` 改用 `feed/profile?url=` 作为首选
2. 首选失败时降级到 `parse_sph?url=`
3. 把两种上游响应 shape 统一映射到现有的 `WxParseData`（即"兼容本地格式"）
4. `POST /wx` 入口的请求/响应 schema 保持不变

**不在本 spec**：

- `POST /wx/finder`（`ParseFinderFeedByObjectID`）的改动——它已经用 `feed/profile?oid=&nid=`，不动
- 上游服务本身的改动
- 重试 / 退避 / 缓存
- `WxParseData` 字段调整或新增
- `api_base_url` 配置项调整

## 架构

### 改动文件清单

| 文件 | 改动 |
|---|---|
| `internal/service/parser.go` | 重写：拆出 `fetchFeedProfile` / `fetchParseSph` 两个端点方法 + `convertObjectShape` / `convertFeedInfoShape` 两个转换函数 + `parseAndConvert` 共用编排。删除 `Parse` 中对 `shared_feed/profile` 的调用。`ParseFinderFeedByObjectID` 不变。 |
| `internal/service/parser_test.go` | 新增：用 `httptest.Server` 模拟两个上游端点，覆盖三种情况（feed 成功 / feed 失败→sph 成功 / 都失败）+ 两种 shape 的字段映射 + 必填字段缺失判负。 |
| `internal/handler/handler.go` | **不改**（`ParseWxURL` 入口不变，仍调用 `h.parser.Parse(req.URL)`） |
| `internal/model/response.go` | **不改**（`WxParseData` JSON 字段不变） |
| `main.go` / `config/*.go` | **不改** |

### 端点调用规约

#### 主：`GET /api/channels/feed/profile?url=<shareURL>`

- Query：`url` 必填
- 响应（成功）：

  ```json
  {
    "code": 0,
    "msg": "...",
    "data": {
      "errCode": 0,
      "errMsg": "...",
      "data": {
        "object": {
          "objectDesc": {
            "description": "标题",
            "media": [
              {
                "url": "https://...",
                "mediaType": 4,
                "decodeKey": "...",
                "urlToken": "?...",
                "coverUrl": "https://..."
              }
            ]
          },
          "contact": { "nickname": "作者" }
        }
      }
    }
  }
  ```

#### 降级：`GET /api/channels/parse_sph?url=<shareURL>`

- Query：`url` 必填；上游还要求 `cloudflare.sphCookie` 已配置（在 wx_channels_download 服务端配置，不在本项目管）
- 响应（成功）：

  ```json
  {
    "code": 0,
    "msg": "ok",
    "data": {
      "errCode": 0,
      "errMsg": "",
      "data": {
        "authorInfo": { "nickname": "作者" },
        "feedInfo": {
          "description": "标题",
          "mediaType": 4,
          "coverUrl": "https://...",
          "videoUrl": "https://...?encfilekey=...&token=...&...",
          "originVideoUrl": "https://...?encfilekey=...&token=..."
        },
        "sceneInfo": { "dynamicExportId": "..." }
      }
    }
  }
  ```

  注：`originVideoUrl` 是 wx_channels_download 服务端用 `cleanVideoURL` 处理过的产物，只保留 `encfilekey` 和 `token` 两个 query 参数。本项目直接取这个值——**必须用 `originVideoUrl`，不要用 `videoUrl`**。`videoUrl` 是 yuanbao 上游返回的原始 URL，带一堆无关的 query 噪音（鉴权/统计/渲染参数等），不保证是原画直链；`originVideoUrl` 是清理后的原画质直链，下载时用它能直接拿到原始文件。

### 响应 → `WxParseData` 映射

| `WxParseData` 字段 | feed/profile (object 形状) | parse_sph (feedInfo 形状) |
|---|---|---|
| `author` | `data.data.object.contact.nickname` | `data.data.authorInfo.nickname` |
| `title` | `data.data.object.objectDesc.description` | `data.data.feedInfo.description` |
| `cover_url` | `data.data.object.objectDesc.media[0].coverUrl` | `data.data.feedInfo.coverUrl` |
| `video_url` | `media[0].url + media[0].urlToken` | `data.data.feedInfo.originVideoUrl` |
| `decode_key` | `media[0].decodeKey` | `""`（parse_sph 不返回；空串 = 该视频无加密） |
| `media_type` | `media[0].mediaType` | `data.data.feedInfo.mediaType` |

### 成功判定

任一端点返回 HTTP 200 + `code==0` + `data.errCode==0`，并且转换后 `author / title / cover_url / video_url` 四个字段全部非空，才算成功。

`media_type == 0` 不算失败（上游有时不填，调用方按 0 处理），但 `media` 数组为空算失败。

### 降级流程

```
Parse(shareURL):
  1. fetchFeedProfile(shareURL) → 尝试转 WxParseData
       任一环节失败（HTTP 错误 / 解析失败 / code!=0 / errCode!=0 /
       必填字段缺失）→ log.Printf 一行 + 继续
  2. fetchParseSph(shareURL) → 尝试转 WxParseData
       同样的成功判定；任一失败 → log.Printf 一行
  3. 任一成功：返回 *WxParseData, nil
  4. 都失败：返回最后一次的 error（保留现有 fmt.Errorf 风格）
```

中间失败用 `log.Printf` 写一行（不上抛到 handler / 调用方），便于排查但不污染响应。最终失败时把最后一次的错误透出，沿用现有错误风格：`fmt.Errorf("请求失败: %w")` / `"API错误: code=%d, msg=%s"` 等。

### 内部结构

```
ParserService
├── Parse(shareURL) -> (*WxParseData, error)         [public, 编排]
├── ParseFinderFeedByObjectID(...) -> (...)          [public, 保持原样]
├── fetchFeedProfile(shareURL) -> (rawBytes, error)  [private, 单一职责]
├── fetchParseSph(shareURL) -> (rawBytes, error)     [private, 单一职责]
├── convertObjectShape(rawBytes) -> (*WxParseData, error)   [private, shape 1]
└── convertFeedInfoShape(rawBytes) -> (*WxParseData, error) [private, shape 2]
```

- `fetch*` 负责 HTTP GET + 返回原始字节，不做业务字段解析
- `convert*` 负责 unmarshal 到对应中间结构 + 校验 + 映射到 `WxParseData`
- `Parse` 只做编排：依次试两个端点，第一个 `convert*` 返回非 nil 即返回，否则继续

### 错误处理

沿用现有 `fmt.Errorf` 中文错误前缀，便于日志搜索：

| 场景 | 错误前缀 |
|---|---|
| HTTP 请求失败 | `feed 请求失败: %w` / `sph 请求失败: %w` |
| Body 读取失败 | `feed 读取响应失败: %w` / `sph 读取响应失败: %w` |
| JSON 解析失败 | `feed 解析响应失败: %w` / `sph 解析响应失败: %w` |
| 外层 `code != 0` | `feed API错误: code=%d, msg=%s` / `sph API错误: code=%d, msg=%s` |
| 内层 `errCode != 0` | `feed 获取feed失败: errCode=%d, errMsg=%s` / `sph 获取feed失败: errCode=%d, errMsg=%s` |
| 数据缺失 | `feed 未找到媒体文件` / `sph feedInfo 为空` / 必填字段缺失时 `feed 必填字段缺失: %s` / `sph 必填字段缺失: %s` |

### 数据流

```
POST /wx { url: "https://weixin.qq.com/sph/A48v1zOJKL" }
  └─> Handler.ParseWxURL
        └─> ParserService.Parse(shareURL)
              ├─> fetchFeedProfile → convertObjectShape → *WxParseData? ✓ → return
              └─ 失败 → fetchParseSph → convertFeedInfoShape → *WxParseData? ✓ → return
                └─ 失败 → return last error
  └─> WxParseResponse { code, msg, data }
```

### 测试

`internal/service/parser_test.go`（新增），用 `net/http/httptest.NewServer` 模拟上游：

| 用例 | 描述 |
|---|---|
| `TestParse_FeedProfile_Success` | feed 返回 object 形状，断言 6 个字段全部正确映射 |
| `TestParse_ParseSph_Success` | sph 返回 feedInfo 形状，断言 6 个字段全部正确映射；`decode_key == ""`；`video_url` 取 `originVideoUrl` 而**不是** `videoUrl`（原画直链约束） |
| `TestParse_FeedFails_ParseSphSucceeds` | feed 返回 `errCode != 0`，sph 返回 200+0+0，最终取 sph 结果 |
| `TestParse_FeedFails_ParseSphFails` | feed 500、sph 500，返回非 nil error |
| `TestParse_FeedReturnsEmptyData` | feed 返回 `data.data.object.objectDesc.media` 为空 → 降级 |
| `TestParse_ParseSphFallsBackOnNetworkError` | feed 服务连接拒绝，sph 200 → 取 sph 结果 |
| `TestParse_RequiredFieldsMissing` | feed 返回的 `contact.nickname == ""` → 视为失败，降级 |
| `TestParseFinderFeedByObjectID_Unchanged` | 回归：原 `ParseFinderFeedByObjectID` 行为不变 |

每个端点用 `httptest.NewServer` 起一个本地 HTTP server，`ParserService` 通过 `config.SetTestApiBaseURL(server.URL)` 之类的方式注入测试 URL（具体方式在实现 plan 里定）。

## 自审

- [x] 没有 TBD / TODO
- [x] 内部一致：`fetch*` 拿原始字节、`convert*` 负责解析，职责不交叉
- [x] 范围聚焦：只动 `parser.go` + 新增测试，不动 handler / model / config / main
- [x] 无歧义："成功判定"、"必填字段"、"降级触发条件" 都写死了
