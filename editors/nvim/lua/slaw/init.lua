-- Slaw for Neovim: `slaw lsp` completion and hover in .sql files under a Slaw
-- workspace (a folder with a slaw.yaml), plus :SlawRun to run the buffer or a
-- selection with `slaw query` and :SlawSnapshot to refresh the catalog.
--
--   require("slaw").setup({ cmd = { "slaw" } })
local M = {}

local defaults = {
  -- The slaw executable (and any leading arguments).
  cmd = { "slaw" },
  -- Filetypes the language server attaches to.
  filetypes = { "sql" },
  -- Extra arguments for :SlawRun, e.g. { "--format", "csv" }.
  run_args = {},
}

M.config = vim.deepcopy(defaults)

-- The workspace root for a buffer: $SLAW_WORKSPACE (set when the TUI or
-- `slaw query --edit` opened this editor), else the nearest slaw.yaml above it.
function M.root(bufnr)
  local env = vim.env.SLAW_WORKSPACE
  if env and env ~= "" then
    return env
  end
  return vim.fs.root(bufnr or 0, "slaw.yaml")
end

local function slaw(args)
  local cmd = vim.list_extend(vim.deepcopy(M.config.cmd), args)
  return cmd
end

local function enable_lsp()
  local spec = {
    cmd = slaw({ "lsp" }),
    filetypes = M.config.filetypes,
    -- Start only inside a workspace: no slaw.yaml, no server.
    root_dir = function(bufnr, on_dir)
      local root = M.root(bufnr)
      if root then
        on_dir(root)
      end
    end,
  }
  if vim.lsp.config and vim.lsp.enable then -- Neovim 0.11+
    vim.lsp.config("slaw", spec)
    vim.lsp.enable("slaw")
    return
  end
  -- Neovim 0.10: start it by hand per buffer.
  vim.api.nvim_create_autocmd("FileType", {
    group = vim.api.nvim_create_augroup("slaw_lsp", { clear = true }),
    pattern = M.config.filetypes,
    callback = function(ev)
      local root = M.root(ev.buf)
      if root then
        vim.lsp.start({ name = "slaw", cmd = spec.cmd, root_dir = root }, { bufnr = ev.buf })
      end
    end,
  })
end

-- Show lines in a reused scratch split.
local function show(lines)
  local name = "slaw://results"
  local buf = vim.fn.bufnr(name)
  if buf == -1 then
    buf = vim.api.nvim_create_buf(false, true)
    vim.api.nvim_buf_set_name(buf, name)
    vim.bo[buf].bufhidden = "hide"
  end
  vim.bo[buf].modifiable = true
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].modifiable = false
  if vim.fn.bufwinnr(buf) == -1 then
    vim.cmd("botright split")
    vim.api.nvim_win_set_buf(0, buf)
    vim.cmd("wincmd p")
  end
end

local function split_lines(s)
  return vim.split((s or ""):gsub("\n$", ""), "\n", { plain = true })
end

-- :SlawRun runs the buffer, or the lines of a range, through `slaw query -`.
function M.run(opts)
  local bufnr = vim.api.nvim_get_current_buf()
  local root = M.root(bufnr)
  if not root then
    vim.notify("slaw: not inside a workspace (no slaw.yaml above this file)", vim.log.levels.WARN)
    return
  end
  local first, last = 0, -1
  if opts and opts.range and opts.range > 0 then
    first, last = opts.line1 - 1, opts.line2
  end
  local sql = table.concat(vim.api.nvim_buf_get_lines(bufnr, first, last, false), "\n")
  local args = vim.list_extend({ "query", "--workspace", root }, M.config.run_args)
  table.insert(args, "-")
  vim.system(slaw(args), { stdin = sql, text = true }, function(res)
    vim.schedule(function()
      local lines = split_lines(res.stdout)
      if res.stderr and res.stderr ~= "" then
        vim.list_extend(lines, split_lines(res.stderr))
      end
      show(lines)
    end)
  end)
end

-- :SlawSnapshot rewrites the catalog snapshot the language server reads.
function M.snapshot()
  local root = M.root(0)
  if not root then
    vim.notify("slaw: not inside a workspace", vim.log.levels.WARN)
    return
  end
  vim.system(slaw({ "lsp", "snapshot", "--workspace", root }), { text = true }, function(res)
    vim.schedule(function()
      local level = res.code == 0 and vim.log.levels.INFO or vim.log.levels.WARN
      vim.notify("slaw: " .. vim.trim((res.stdout or "") .. (res.stderr or "")), level)
    end)
  end)
end

function M.setup(opts)
  M.config = vim.tbl_deep_extend("force", vim.deepcopy(defaults), opts or {})
  enable_lsp()
  vim.api.nvim_create_user_command("SlawRun", M.run, { range = true, desc = "Run SQL with slaw query" })
  vim.api.nvim_create_user_command("SlawSnapshot", M.snapshot, { desc = "Refresh the Slaw catalog snapshot" })
end

return M
