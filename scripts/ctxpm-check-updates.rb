#!/usr/bin/env ruby
# frozen_string_literal: true

require 'json'
require_relative 'ctxpm_update_support'

options = {
  manifest_path: File.expand_path('ctxpm.yaml', Dir.pwd),
  force: false,
  json: false
}
json_requested = ARGV.include?('--json')
json_requested = ARGV.include?('--json')

parser = OptionParser.new do |opts|
  opts.banner = 'Usage: scripts/ctxpm-check-updates [options]'

  opts.on('--manifest PATH', 'Path to ctxpm.yaml (default: ./ctxpm.yaml)') do |value|
    options[:manifest_path] = File.expand_path(value, Dir.pwd)
  end

  opts.on('--state PATH', 'Path to update state JSON (default: <project>/.ctxpm/state/update-checks.json)') do |value|
    options[:state_path] = File.expand_path(value, Dir.pwd)
  end

  opts.on('--force', 'Run the check even if the configured interval is not due yet') do
    options[:force] = true
  end

  opts.on('--json', 'Print machine-readable JSON output') do
    options[:json] = true
  end
end

begin
  begin
    parser.parse!(ARGV)
    options[:state_path] ||= CtxpmUpdateSupport.default_state_path(options[:manifest_path])

    payload = CtxpmUpdateSupport.build_check_payload(
      manifest_path: options[:manifest_path],
      state_path: options[:state_path],
      force: options[:force]
    )

    puts(options[:json] ? JSON.pretty_generate(payload) : CtxpmUpdateSupport.render_check_payload(payload))
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
