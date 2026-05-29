package model

type WxParseRequest struct {
    URL string `json:"url" binding:"required"`
}

type FinderFeedRequest struct {
    ObjectID      string `json:"objectId"`
    ObjectNonceID string `json:"objectNonceId"`
    Nickname      string `json:"nickname"`
    Desc          string `json:"desc"`
}

type WxParseData struct {
    Author    string `json:"author"`
    Title     string `json:"title"`
    CoverURL  string `json:"cover_url"`
    VideoURL  string `json:"video_url"`
    DecodeKey string `json:"decode_key"`
    MediaType int    `json:"media_type"`
}

type WxParseResponse struct {
    Code int          `json:"code"`
    Msg  string       `json:"msg"`
    Data *WxParseData `json:"data"`
}

type SimpleResponse struct {
    Code int    `json:"code"`
    Msg  string `json:"msg"`
}