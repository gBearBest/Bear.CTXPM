#!/usr/bin/env ruby
# frozen_string_literal: true

require 'json'
require_relative 'ctxpm_update_support'

options = {
  manifest_path: File.expand_path('ctxpm.yaml', Dir.pwd),
  apply_all: false,
  dry_run: false,
  json: false
}
json_requested = ARGV.include?('--json')

parser = OptionParser.new do |opts|
  opts.banner = 'Usage: scripts/ctxpm-apply-updates [options] [dependency-name ...]'

  opts.on('--manifest PATH', 'Path to ctxpm.yaml (default: ./ctxpm.yaml)') do |value|
    options[:manifest_path] = File.expand_path(value, Dir.pwd)
  end

  opts.on('--state PATH', 'Path to update state JSON (default: <project>/.ctxpm/state/update-checks.json)') do |value|
    options[:state_path] = File.expand_path(value, Dir.pwd)
  end

  opts.on('--all', 'Apply every currently available dependency update') do
    options[:apply_all] = true
  end

  opts.on('--dry-run', 'Resolve and report updates without changing files') do
    options[:dry_run] = true
  end

  opts.on('--json', 'Print machine-readable JSON output') do
    options[:json] = true
  end
end

begin
  parser.parse!(ARGV)
  options[:state_path] ||= CtxpmUpdateSupport.default_state_path(options[:manifest_path])

  payload = CtxpmUpdateSupport.apply_updates(
    manifest_path: options[:manifest_path],
    state_path: options[:state_path],
    names: ARGV,
    apply_all: options[:apply_all],
    dry_run: options[:dry_run]
  )

  puts(options[:json] ? JSON.pretty_generate(payload) : CtxpmUpdateSupport.render_apply_payload(payload))
rescue OptionParser::ParseError => e
  if options[:json] || json_requested
    puts JSON.pretty_generate(
      'status' => 'error',
      'reason' => e.message
    )
  else
    warn e.message
    warn parser.banner
  end
  exit 1
rescue CtxpmUpdateSupport::ScriptError => e
  if options[:json] || json_requested
    puts JSON.pretty_generate(
      'status' => 'error',
      'reason' => e.message
    )
  else
    warn e.message
  end
  exit 1
end
