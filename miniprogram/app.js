App({
  onLaunch() {
    const settings = wx.getStorageSync("reliquary_settings");
    if (!settings) {
      wx.setStorageSync("reliquary_settings", {
        baseUrl: "http://localhost:8080",
        apiKey: "",
        selectedKnowledgeBaseId: ""
      });
    }
  }
});
