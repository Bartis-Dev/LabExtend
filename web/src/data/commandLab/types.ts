// Documentation-style data model for Command Lab. Categories live in
// the sidebar; each one renders a sequence of topic Sections, each with
// a short description and one or more example commands plus optional
// tips and important file paths.

export type Example = {
  command: string;
  note?: string;
};

export type Section = {
  id: string;
  title: string;
  description?: string;
  examples?: Example[];
  paths?: { path: string; description: string }[];
  tip?: string;
  warning?: string;
};

export type Category = {
  id: string;       // unique slug across the whole catalogue
  shell: string;    // grouping label in the sidebar (e.g. "Linux")
  label: string;    // category title (e.g. "Permissions")
  icon: string;     // lucide icon name
  description?: string;
  sections: Section[];
};
