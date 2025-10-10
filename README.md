

# AI Recipe API

AI 智能食譜 API，支援食譜生成、食材/設備/食物圖片辨識，具備高效快取、速率限制、健康檢查與 Docker 部署。  
**所有 API 輸入/輸出嚴格遵循 OpenAPI（recipe-api.yaml）規格。**

---

## 目錄結構與模組說明

```
.
├── cmd/api/                  # 主程式入口 (main.go)
├── internal/
│   ├── api/                  # API 層：路由、handler、中間件
│   │   ├── handlers/         # 各功能 handler（recipe, health, ...）
│   │   ├── middleware/       # 請求日誌、限流、去重等中間件
│   │   └── router.go         # 路由註冊
│   ├── core/
│   │   ├── ai/               # AI 服務、快取、OpenRouter 整合
│   │   │   ├── cache/        # 記憶體快取（LRU/TTL）
│   │   │   ├── openrouter/   # OpenRouter API 封裝
│   │   │   ├── provider/     # AI 供應商抽象
│   │   │   ├── queue/        # 請求佇列
│   │   │   └── service/      # AI 請求服務
│   │   └── recipe/           # 食譜、食材、食物業務邏輯
│   └── infrastructure/       # 設定載入、共用工具
├── recipe-api.yaml           # OpenAPI 規格（API schema 定義）
├── Dockerfile                # 多階段建構，含健康檢查
├── .env.example              # 環境變數範例
├── docker-compose.yml        # 多服務協同部署（如有）
└── README.md
```

---

## 專案設計理念

- **嚴格 API Schema 驗證**：所有 handler 輸入/輸出皆與 OpenAPI 規格完全一致，便於前後端協作與自動化測試。
- **AI 驅動**：整合 OpenRouter（Google Gemini）模型，確保食譜生成與辨識結果具備高品質與彈性。
- **高效快取與限流**：純記憶體快取（無外部 Redis），支援 TTL、LRU、請求去重與速率限制，保證高併發下的穩定性。
- **現代化日誌**：多級日誌、中文標題、避免敏感/大資料外洩，方便除錯與維運。
- **健康檢查與自動監控**：/health、/ready、/live 路由，Docker HEALTHCHECK，便於雲端部署與自動化監控。
- **可擴展性**：所有業務邏輯、AI 供應商、快取、限流皆可獨立擴充。

---

## 技術選型

- **Go 1.21+**：高效能、易維護
- **Gin**：API 路由與中間件
- **OpenAPI 3.0**：API schema 驗證與自動文件
- **OpenRouter**：AI 服務（Google Gemini）
- **Zap**：高效結構化日誌
- **In-memory LRU/TTL Cache**：極速快取
- **Docker**：一致性部署
- **.env**：環境變數集中管理

---

## 主要功能

- **AI 食譜生成**：根據食材、偏好自動產生詳細新手友善食譜
- **圖片辨識**：支援食物、食材、設備圖片辨識
- **高效快取**：純記憶體快取，支援 TTL、LRU
- **速率限制**：可設定請求速率與去重時間窗
- **健康檢查**：/health、/ready、/live 路由，Docker HEALTHCHECK
- **多級日誌**：info/debug/error，中文標題，避免敏感/大資料外洩
- **OpenRouter (Google Gemini) AI 整合**

---

## 快速開始

### 1. 環境需求

- Go 1.21+
- Docker（建議部署用）

### 2. 安裝與啟動

#### 本地開發

```bash
git clone <your-repo-url>
cd <project-folder>
cp .env.example .env
# 編輯 .env，填入 OpenRouter API Key 等必要參數
go run cmd/api/main.go
```
服務預設於 [http://\<IP\>:8080](http://<IP>:8080) 運行。

#### Docker 部署

```bash
docker build -t recipe-generator .
docker run -d --name recipe-generator-api -p 8080:8080 -v $(pwd)/.env:/app/.env recipe-generator
//背景運行
docker run --name recipe-generator-api -p 8080:8080 -v $(pwd)/.env:/app/.env recipe-generator
//前台運行
```

#### Docker Compose

```bash
docker-compose up --build -d
```

---

## API 文件

- OpenAPI 規格：`recipe-api.yaml`
- 預覽建議：[Swagger Editor](https://editor.swagger.io/) 或
  ```bash
  docker run -p 8081:8080 -v $PWD/recipe-api.yaml:/swagger.yaml swaggerapi/swagger-ui
  # 瀏覽 http://localhost:8081
  ```

### 主要端點

- `POST /api/v1/recipe/food` — 圖片辨識食物
- `POST /api/v1/recipe/ingredient` — 圖片辨識食材與設備
- `POST /api/v1/recipe/generate` — 依據名稱/偏好生成詳細食譜
- `POST /api/v1/recipe/suggest` — 根據食材/設備推薦食譜
- `POST /api/v1/cook/qa` — 烹調過程即時問答
- `GET /health` `/ready` `/live` — 健康檢查

**所有 API 輸入/輸出皆嚴格遵循 OpenAPI schema，請參考 `recipe-api.yaml`。**

---

## API 欄位說明與範例

### 1. 食物圖片辨識

**請求**
```json
POST /api/v1/recipe/food
{
  "image": "data:image/jpeg;base64,...",
  "description_hint": "一盤炒飯"
}
```
**回應**
```json
{
  "recognized_foods": [
    {
      "name": "炒飯",
      "description": "經典中式炒飯",
      "possible_ingredients": [
        { "name": "米飯", "type": "主食" },
        { "name": "蛋", "type": "蛋類" }
      ],
      "possible_equipment": [
        { "name": "炒鍋", "type": "鍋具" }
      ]
    }
  ]
}
```
- `image`：支援 base64 或 URL
- `description_hint`：可選，輔助 AI 辨識

### 2. 食材/設備圖片辨識

**請求**
```json
POST /api/v1/recipe/ingredient
{
  "image": "data:image/png;base64,...",
  "description_hint": "蔬菜和鍋子"
}
```
**回應**
```json
{
  "ingredients": [
    { "name": "青江菜", "type": "蔬菜", "amount": "2", "unit": "株", "preparation": "洗淨" }
  ],
  "equipment": [
    { "name": "炒鍋", "type": "鍋具", "size": "中型", "material": "鐵", "power_source": "瓦斯" }
  ],
  "summary": "包含蔬菜與鍋具"
}
```

### 3. 依名稱/偏好生成食譜

**請求**
```json
POST /api/v1/recipe/generate
{
  "dish_name": "番茄炒蛋",
  "preferred_ingredients": ["番茄", "蛋"],
  "excluded_ingredients": ["蔥"],
  "preferred_equipment": ["炒鍋"],
  "preference": {
    "cooking_method": "炒",
    "doneness": "全熟",
    "serving_size": "2人份"
  }
}
```
**回應**
```json
{
  "dish_name": "番茄炒蛋",
  "dish_description": "經典家常菜，酸甜開胃",
  "ingredients": [
    { "name": "番茄", "type": "蔬菜", "amount": "2", "unit": "顆", "preparation": "切塊" },
    { "name": "蛋", "type": "蛋類", "amount": "3", "unit": "顆", "preparation": "打散" }
  ],
  "equipment": [
    { "name": "炒鍋", "type": "鍋具", "size": "中型", "material": "鐵", "power_source": "瓦斯" }
  ],
  "recipe": [
    {
      "step_number": 1,
      "ARtype": "cut",
      "ar_parameters": {
        "type": "cut",
        "ingredient": "tomato"
      },
      "title": "備料",
      "description": "將番茄切塊，蛋打散備用。",
      "actions": [
        {
          "action": "切塊",
          "tool_required": "刀",
          "material_required": ["番茄"],
          "time_minutes": 2,
          "instruction_detail": "番茄切成適口大小"
        }
      ],
      "estimated_total_time": "2分鐘",
      "temperature": "常溫",
      "warnings": "無",
      "notes": "無備註"
    }
  ]
}
```
- `preference` 欄位可細緻指定烹飪方式、熟度、份量

### 4. 根據食材/設備推薦食譜

**請求**
```json
POST /api/v1/recipe/suggest
{
  "available_ingredients": [
    { "name": "蛋", "type": "蛋類", "amount": "2", "unit": "顆", "preparation": "打散" }
  ],
  "available_equipment": [
    { "name": "平底鍋", "type": "鍋具", "size": "小型", "material": "不沾", "power_source": "電" }
  ],
  "preference": {
    "cooking_method": "煎",
    "dietary_restrictions": ["無麩質"],
    "serving_size": "1人份"
  }
}
```
**回應**
```json
{
  "dish_name": "煎蛋",
  "dish_description": "簡單快速的早餐料理",
  "ingredients": [
    { "name": "蛋", "type": "蛋類", "amount": "2", "unit": "顆", "preparation": "打散" }
  ],
  "equipment": [
    { "name": "平底鍋", "type": "鍋具", "size": "小型", "material": "不沾", "power_source": "電" }
  ],
  "recipe": [
    {
      "step_number": 1,
      "ARtype": "stir",
      "ar_parameters": {
        "type": "stir",
        "container": "pan",
        "ingredient": "egg"
      },
      "title": "煎蛋",
      "description": "將蛋液倒入鍋中，小火煎熟。",
      "actions": [
        {
          "action": "煎",
          "tool_required": "平底鍋",
          "material_required": ["蛋"],
          "time_minutes": 3,
          "instruction_detail": "蛋液均勻攤平"
        }
      ],
      "estimated_total_time": "3分鐘",
      "temperature": "小火",
      "warnings": "無",
      "notes": "可加鹽調味"
    }
  ]
}
```

---

### 5. Cook 問答（Cook QA）

**用途**：帶入使用者問題、當前步驟描述、料理圖片與完整食譜，讓 AI 即時回覆烹調疑問。

**請求**
```json
POST /api/v1/cook/qa
{
  "question": "如何避免番茄炒蛋時番茄出水太多？",
  "current_step_description": "正在翻炒番茄與雞蛋，發現汁水偏多",
  "image": "data:image/png;base64,...",
  "recipe": {
    "dish_name": "番茄炒蛋",
    "dish_description": "經典家常菜，酸甜開胃",
    "ingredients": [
      { "name": "番茄", "type": "蔬菜", "amount": "2", "unit": "顆", "preparation": "切塊" },
      { "name": "蛋", "type": "蛋類", "amount": "3", "unit": "顆", "preparation": "打散" }
    ],
    "equipment": [
      { "name": "炒鍋", "type": "鍋具", "size": "中型", "material": "鐵", "power_source": "瓦斯" }
    ],
    "recipe": [
      {
        "step_number": 1,
        "ARtype": "cut",
        "ar_parameters": {
          "type": "cut",
          "ingredient": "tomato"
        },
        "title": "備料",
        "description": "將番茄切塊，蛋打散備用。",
        "actions": [
          {
            "action": "切塊",
            "tool_required": "刀",
            "material_required": ["番茄"],
            "time_minutes": 2,
            "instruction_detail": "番茄切成適口大小"
          }
        ],
        "estimated_total_time": "2分鐘",
        "temperature": "常溫",
        "warnings": "無",
        "notes": "無備註"
      }
    ]
  }
}
```

**回應**
```json
{
  "answer": "先將番茄去籽並快速翻炒，避免長時間悶煮造成出水。先炒蛋再下番茄，可降低水分對蛋體口感的影響。"
}
```

### AR 擴增實境欄位

- `POST /api/v1/recipe/generate` 與 `POST /api/v1/recipe/suggest` 的每個步驟都會回傳 `ARtype` 與 `ar_parameters`，欄位格式與 `recipe-api.yaml` 完全一致。
- 服務會先嘗試讓模型產出合法的 AR 參數，若驗證失敗再進行嚴格重試，最後才回退到後端推論，確保兩個端點的 AR 行為一致。
- `ar_parameters.ingredient` 會優先套用白名單中的英文識別名稱（例如 `bacon`, `chickenThigh`, `green_pepper`）。
- 若識別名稱包含多個單字，會以英文逗號 `,` 分隔，禁止使用底線 `_` 或泛用詞（如 `ingredient`, `food`）。
- 新增固定對應的食材時，請更新 `internal/core/recipe/suggestion_service.go` 內的 `canonicalIngredientMap`，鍵使用小寫匹配，值為最終輸出的識別名稱。

---

## 健康檢查 API 回應格式

### /health
```json
{
  "status": "ok",
  "timestamp": "2024-06-01T12:00:00Z",
  "version": "1.0.0",
  "runtime": {
    "goroutines": 8,
    "memory": {
      "alloc": 12345678,
      "total_alloc": 23456789,
      "sys": 34567890,
      "num_gc": 12
    }
  }
}
```

### /ready
```json
{ "status": "ready" }
```

### /live
```json
{ "status": "alive" }
```

---

## 主要環境變數（.env）說明

| 變數名稱 | 說明 | 範例/預設值 |
|---|---|---|
| PORT | 服務監聽埠號 | 8080 |
| APP_OPENROUTER_API_KEY | OpenRouter API 金鑰 | your-api-key-here |
| APP_OPENROUTER_MODEL | 預設 AI 模型 | google/gemini-2.0-flash-001 |
| CACHE_ENABLED | 是否啟用快取 | true |
| CACHE_MAX_SIZE | 快取最大數量 | 1000 |
| CACHE_TTL | 單筆快取有效時間 | 1h |
| RATE_LIMIT_ENABLED | 是否啟用速率限制 | true |
| RATE_LIMIT_REQUESTS | 每視窗最大請求數 | 100 |
| RATE_LIMIT_WINDOW | 限流視窗大小 | 1m |
| DEDUP_WINDOW | 請求去重時間窗 | 500ms |
| LOG_LEVEL | 日誌等級 | info |
| APP_ENV | 執行環境 | development |
| APP_DEBUG | 是否啟用 debug | true |
| ... | 其餘請參考 .env.example |  |

---

## 日誌策略

- **info**：僅記錄請求摘要、標題、狀態
- **debug**：詳細記錄 AI 請求/回應（不含圖片/敏感資料）
- **error**：錯誤堆疊、AI 解析失敗、外部服務異常
- **中文標題**：所有 log 標題皆為中文，方便維運
- **避免大資料外洩**：圖片、AI 回應大欄位不進 info log

---

## 快取、限流、去重設計細節

- **快取**：純記憶體 LRU+TTL，依 .env 設定最大數量與存活時間
- **限流**：每個 API 可依 .env 設定速率與視窗
- **請求去重**：同一內容 POST 請求於 DEDUP_WINDOW 內只處理一次
- **所有參數皆可熱調整**（重啟生效）

---

## 錯誤處理

- **API 回應皆為標準 JSON**，包含 error code、訊息、細節
- **AI 回應格式錯誤**：自動偵測 markdown、截斷、型別不符，並回傳友善錯誤
- **健康檢查失敗**：/health 會回傳詳細錯誤與 runtime 狀態

---

## 部署建議

- 建議使用 Docker 或 Docker Compose 部署，確保環境一致性。
- Dockerfile 已設置 HEALTHCHECK，Kubernetes/雲端平台可自動監控存活。
- 建議將 .env 檔案與敏感資訊妥善管理，不要提交到公開倉庫。
- 可用 Nginx 等反向代理加強安全性與流量管理。
- 支援多執行緒與高併發，適合雲端自動擴展

---

## 開發流程與貢獻指南

1. Fork 專案
2. 創建功能分支（feature/xxx）
3. 撰寫/修改程式碼，確保符合 OpenAPI schema
4. 執行 `gofmt` 格式化
5. 本地測試（go run/curl 測 API）
6. 發起 Pull Request，描述修改內容與動機

---

## 常見問題 FAQ

**Q: AI 回應格式錯誤或 JSON 解析失敗？**  
- 請檢查 prompt 是否明確要求 AI 嚴格回傳 JSON，或調整模型參數（如 temperature）。
- 查看 debug log 取得 AI 原始回應內容。

**Q: 為什麼健康檢查顯示 unhealthy？**  
- 請確認服務有正常啟動，且 /health 路由可被存取。
- 檢查 Docker log 與 .env 設定。

**Q: 如何擴充 API？**  
- 於 internal/api/handlers/recipe/ 新增 handler，並於 router.go 註冊路由。
- schema 需同步更新 recipe-api.yaml。

**Q: 如何自訂 AI 供應商或模型？**  
- 修改 .env 的 APP_OPENROUTER_MODEL，或擴充 internal/core/ai/provider/。

---

## 授權

MIT License

---

如需協助或有建議，歡迎提 issue 或聯絡作者。

---

如需更詳細的 API schema、請參考 `recipe-api.yaml` 及原始碼註解。
