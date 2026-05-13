// Knowledge-base shape for Command Lab.
//
// A Shell groups Commands and FilePaths. Each Command has a `template`
// string with `{key}` placeholders that are filled from the builder
// form: flags are gathered into `{flags}`, named args go to their own
// `{argkey}` slots.

export type FlagType = 'bool' | 'string' | 'number' | 'enum';

export type Flag = {
  key: string;
  flag: string;          // CLI form, e.g. "-r" or "--recursive"
  description: string;
  type: FlagType;
  default?: string | number | boolean;
  options?: string[];    // for enum
  placeholder?: string;
};

export type Argument = {
  key: string;
  name: string;          // label shown in the form
  description: string;
  required?: boolean;
  placeholder?: string;
};

export type Command = {
  id: string;            // stable id within shell
  name: string;          // displayed name, e.g. "chown"
  description: string;
  category: string;
  template: string;      // e.g. "chown {flags} {owner}:{group} {path}"
  args?: Argument[];
  flags?: Flag[];
  examples?: string[];
  notes?: string;
};

export type FilePath = {
  path: string;
  description: string;
  category: string;
};

export type Shell = {
  id: string;
  label: string;
  description: string;
  icon: string;          // lucide name
  commands: Command[];
  paths: FilePath[];
};
