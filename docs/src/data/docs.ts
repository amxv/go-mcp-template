export const siteConfig = {
  name: "Origo",
  strapline: "Source-first access for agents",
  description:
    "Origo is a minimal Go MCP server for direct website retrieval and site mapping. Infrastructure is ready; retrieval features are in design.",
  repoUrl: "https://github.com/amxv/origo",
  accentColor: "#0369a1",
  accentColorDark: "#38bdf8",
  footerSections: [
    {
      title: "Origo",
      text: "A small, source-first MCP service for agents. No search engine, no CLI distribution."
    },
    {
      title: "Project status",
      text: "Transport and deployments are being established. Read-link and site mapping are planned, not yet implemented."
    },
    {
      title: "Repository",
      linkPrefix: "Source: ",
      linkHref: "https://github.com/amxv/origo",
      linkLabel: "github.com/amxv/origo (private)"
    }
  ]
} as const;

export const docCategories = ["Start", "Design", "Operations"] as const;

export const primaryNav = [
  { href: "/docs", label: "Docs" },
  { href: siteConfig.repoUrl, label: "GitHub", external: true }
];
