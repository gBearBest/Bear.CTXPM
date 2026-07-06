# frozen_string_literal: true

require 'cgi'
require 'digest'
require 'fileutils'
require 'json'
require 'open-uri'
require 'open3'
require 'optparse'
require 'pathname'
require 'tmpdir'
require 'time'
require 'yaml'

module CtxpmUpdateSupport
  USER_AGENT = 'Bear.CTXPM ctxpm update scripts'.freeze
  DEFAULT_POLICY = {
    'enabled' => true,
    'interval' => '1d',
    'include_self' => true
  }.freeze

  class ScriptError < StandardError; end

  class Dependency
    attr_reader :raw, :project_root

    def initialize(raw, project_root)
      @raw = raw
      @project_root = project_root
    end

    def name
      raw['name']
    end

    def type
      raw['type']
    end

    def version
      raw['version']
    end

    def source
      raw['source'] || {}
    end

    def source_type
      source['type']
    end

    def canonical_path
      raw['path']
    end

    def absolute_path
      File.expand_path(canonical_path, project_root)
    end

    def compatibility_paths
      Array(raw['compatibility'])
    end
  end

  module_function

  def load_manifest(manifest_path)
    manifest = YAML.load_file(manifest_path)
    raise ScriptError, "Manifest at #{manifest_path} did not contain a YAML mapping." unless manifest.is_a?(Hash)

    manifest
  rescue Errno::ENOENT
    raise ScriptError, "Manifest not found: #{manifest_path}"
  rescue Psych::SyntaxError => e
    raise ScriptError, "Failed to parse #{manifest_path}: #{e.message}"
  end

  def project_root_for(manifest_path)
    File.expand_path(File.dirname(manifest_path))
  end

  def default_state_path(manifest_path)
    File.join(project_root_for(manifest_path), '.ctxpm', 'state', 'update-checks.json')
  end

  def load_policy(manifest)
    policy = manifest['update_policy']
    return DEFAULT_POLICY.dup unless policy.is_a?(Hash)

    DEFAULT_POLICY.merge(policy)
  end

  def truthy?(value)
    return value if value == true || value == false
    return false if value.nil?

    %w[1 true yes on].include?(value.to_s.strip.downcase)
  end

  def parse_interval(interval_text)
    text = interval_text.to_s.strip
    raise ScriptError, 'update_policy.interval must not be empty.' if text.empty?

    match = /\A(\d+)([smhd])\z/.match(text)
    raise ScriptError, "Unsupported update_policy.interval #{interval_text.inspect}. Use forms like 12h or 1d." unless match

    amount = match[1].to_i
    multiplier = case match[2]
                 when 's' then 1
                 when 'm' then 60
                 when 'h' then 3600
                 when 'd' then 86_400
                 end

    amount * multiplier
  end

  def read_state(state_path)
    return {} unless File.exist?(state_path)

    JSON.parse(File.read(state_path))
  rescue JSON::ParserError => e
    raise ScriptError, "Failed to parse state file #{state_path}: #{e.message}"
  end

  def write_state(state_path, payload)
    FileUtils.mkdir_p(File.dirname(state_path))
    File.write(state_path, JSON.pretty_generate(payload) + "\n")
  end

  def dependency_list(manifest, manifest_path)
    root = project_root_for(manifest_path)
    Array(manifest['dependencies']).map do |entry|
      next unless entry.is_a?(Hash)

      Dependency.new(entry, root)
    end.compact
  end

  def parse_github_repo(url)
    path = URI.parse(url).path.to_s.sub(%r{\A/}, '').sub(/\.git\z/, '')
    owner, repo, = path.split('/')
    raise ScriptError, "Unsupported GitHub repository URL #{url.inspect}" if owner.nil? || repo.nil?

    [owner, repo]
  rescue URI::InvalidURIError
    raise ScriptError, "Invalid GitHub repository URL #{url.inspect}"
  end

  def open_text(url)
    URI.open(url, 'User-Agent' => USER_AGENT, 'Accept' => 'application/vnd.github+json', &:read)
  rescue OpenURI::HTTPError => e
    raise ScriptError, "Request to #{url} failed: #{e.message}"
  rescue SocketError, Errno::ECONNREFUSED, Errno::ETIMEDOUT => e
    raise ScriptError, "Network request to #{url} failed: #{e.message}"
  end

  def resolve_dependency(dependency)
    case dependency.source_type
    when 'github'
      resolve_github_dependency(dependency)
    when 'url'
      resolve_url_dependency(dependency)
    else
      unresolved_result(dependency, "Unsupported or missing source.type #{dependency.source_type.inspect}.")
    end
  end

  def resolve_github_dependency(dependency)
    source = dependency.source
    url = source['url']
    path = source['path']

    return unresolved_result(dependency, 'GitHub dependency is missing source.url.') if blank?(url)
    return unresolved_result(dependency, 'GitHub dependency is missing source.path.') if blank?(path)
    return unresolved_result(dependency, 'GitHub dependency is missing version.') if blank?(dependency.version)

    latest_version = latest_github_version_via_api(url: url, path: path, ref: source['ref'])
    latest_version = latest_github_version_via_git(url: url, path: path, ref: source['ref']) if blank?(latest_version)
    return unresolved_result(dependency, 'Could not resolve the latest GitHub revision for the configured source path/ref.') if blank?(latest_version)

    checked_result(dependency, latest_version)
  rescue ScriptError => e
    unresolved_result(dependency, e.message)
  rescue JSON::ParserError => e
    unresolved_result(dependency, "GitHub API response for #{dependency.name} was not valid JSON: #{e.message}")
  end

  def latest_github_version_via_api(url:, path:, ref:)
    owner, repo = parse_github_repo(url)
    query = ["path=#{CGI.escape(path)}", 'per_page=1']
    query << "sha=#{CGI.escape(ref)}" unless blank?(ref)
    payload = JSON.parse(open_text("https://api.github.com/repos/#{owner}/#{repo}/commits?#{query.join('&')}"))
    payload.dig(0, 'sha')
  rescue ScriptError
    nil
  end

  def latest_github_version_via_git(url:, path:, ref:)
    Dir.mktmpdir('ctxpm-github-check-') do |tmpdir|
      repo_dir = File.join(tmpdir, 'repo')
      command = ['git', 'clone', '--quiet']
      command += ['--branch', ref] unless blank?(ref)
      command += [url, repo_dir]
      run_command(*command)
      latest_version = run_command('git', '-C', repo_dir, 'log', '-1', '--format=%H', 'HEAD', '--', path).strip
      return latest_version unless blank?(latest_version)
    end

    nil
  rescue ScriptError
    nil
  end

  def resolve_url_dependency(dependency)
    source = dependency.source
    url = source['url']

    return unresolved_result(dependency, 'URL dependency is missing source.url.') if blank?(url)
    return unresolved_result(dependency, 'URL dependency is missing version.') if blank?(dependency.version)

    content = open_text(url)
    latest_version = "sha256:#{Digest::SHA256.hexdigest(content)}"
    checked_result(dependency, latest_version)
  rescue ScriptError => e
    unresolved_result(dependency, e.message)
  end

  def checked_result(dependency, latest_version, extra = {})
    {
      'name' => dependency.name,
      'type' => dependency.type,
      'path' => dependency.canonical_path,
      'source_type' => dependency.source_type,
      'current_version' => dependency.version,
      'latest_version' => latest_version,
      'status' => latest_version == dependency.version ? 'up_to_date' : 'update_available',
      'compatibility' => dependency.compatibility_paths
    }.merge(extra)
  end

  def unresolved_result(dependency, reason)
    {
      'name' => dependency.name,
      'type' => dependency.type,
      'path' => dependency.canonical_path,
      'source_type' => dependency.source_type,
      'current_version' => dependency.version,
      'latest_version' => nil,
      'status' => 'unresolved',
      'reason' => reason,
      'compatibility' => dependency.compatibility_paths
    }
  end

  def build_check_payload(manifest_path:, state_path:, force: false, persist: true)
    manifest = load_manifest(manifest_path)
    policy = load_policy(manifest)
    checked_at = Time.now.utc
    interval = policy['interval'] || DEFAULT_POLICY['interval']

    payload = {
      'manifest_path' => manifest_path,
      'state_path' => state_path,
      'checked_at' => checked_at.iso8601,
      'policy' => {
        'enabled' => truthy?(policy['enabled']),
        'interval' => interval,
        'include_self' => truthy?(policy['include_self'])
      }
    }

    unless truthy?(policy['enabled'])
      payload['status'] = 'disabled'
      payload['dependencies'] = []
      payload['summary'] = summary_for([])
      write_state(state_path, state_payload(payload)) if persist
      return payload
    end

    interval_seconds = parse_interval(interval)
    state = read_state(state_path)
    last_checked_at = parse_time(state['last_full_check_at'])
    if !force && last_checked_at && (checked_at - last_checked_at) < interval_seconds
      payload['status'] = 'not_due'
      payload['last_full_check_at'] = last_checked_at.iso8601
      payload['next_check_at'] = (last_checked_at + interval_seconds).iso8601
      payload['dependencies'] = Array(state['dependencies'])
      payload['summary'] = summary_for(payload['dependencies'])
      return payload
    end

    dependencies = dependency_list(manifest, manifest_path)
    dependencies.reject! { |dependency| dependency.name == 'ctxpm' && !truthy?(policy['include_self']) }
    results = dependencies.map { |dependency| resolve_dependency(dependency) }

    payload['status'] = 'checked'
    payload['dependencies'] = results
    payload['summary'] = summary_for(results)
    write_state(state_path, state_payload(payload)) if persist
    payload
  end

  def state_payload(payload)
    {
      'schema_version' => 1,
      'last_full_check_at' => payload['checked_at'],
      'policy' => payload['policy'],
      'status' => payload['status'],
      'dependencies' => payload['dependencies'],
      'summary' => payload['summary']
    }
  end

  def parse_time(value)
    return nil if blank?(value)

    Time.parse(value)
  rescue ArgumentError
    nil
  end

  def summary_for(results)
    summary = {
      'checked' => 0,
      'update_available' => 0,
      'up_to_date' => 0,
      'unresolved' => 0
    }

    Array(results).each do |result|
      summary['checked'] += 1
      status = result['status']
      summary[status] += 1 if summary.key?(status)
    end

    summary
  end

  def render_check_payload(payload)
    lines = []
    lines << "Update check status: #{payload['status']}"
    lines << "Checked at: #{payload['checked_at']}" if payload['checked_at']
    lines << "Next check at: #{payload['next_check_at']}" if payload['next_check_at']

    summary = payload['summary'] || {}
    lines << "Dependencies inspected: #{summary['checked'] || 0}"
    lines << "Updates available: #{summary['update_available'] || 0}"
    lines << "Unresolved: #{summary['unresolved'] || 0}"

    Array(payload['dependencies']).each do |dependency|
      line = "- #{dependency['name']} [#{dependency['status']}]"
      line += " current=#{dependency['current_version']}" if dependency['current_version']
      line += " latest=#{dependency['latest_version']}" if dependency['latest_version']
      line += " reason=#{dependency['reason']}" if dependency['reason']
      lines << line
    end

    lines.join("\n")
  end

  def blank?(value)
    value.nil? || value.to_s.strip.empty?
  end

  def find_dependency(manifest, manifest_path, name)
    dependency_list(manifest, manifest_path).find { |dependency| dependency.name == name }
  end

  def ensure_relative_symlink(link_path, target_path)
    FileUtils.mkdir_p(File.dirname(link_path))
    relative_target = Pathname.new(target_path).relative_path_from(Pathname.new(File.dirname(link_path))).to_s

    if File.symlink?(link_path)
      current_target = File.readlink(link_path)
      return if current_target == relative_target

      File.delete(link_path)
    elsif File.exist?(link_path)
      raise ScriptError, "Compatibility path #{link_path} already exists and is not a symlink."
    end

    File.symlink(relative_target, link_path)
  end

  def replace_canonical_path(source_path, dest_path)
    FileUtils.rm_rf(dest_path)
    FileUtils.mkdir_p(File.dirname(dest_path))

    if File.directory?(source_path)
      FileUtils.cp_r(source_path, dest_path)
    else
      FileUtils.cp(source_path, dest_path)
    end
  end

  def canonical_target_for_url_dependency(dependency)
    source = dependency.source
    entry = source['entry']
    canonical_path = dependency.absolute_path

    if File.directory?(canonical_path) || canonical_path.end_with?(File::SEPARATOR)
      raise ScriptError, "Dependency #{dependency.name} needs source.entry to write into directory #{dependency.canonical_path}." if blank?(entry)

      File.join(canonical_path, entry)
    else
      canonical_path
    end
  end

  def run_command(*command)
    stdout, stderr, status = Open3.capture3(*command)
    return stdout if status.success?

    raise ScriptError, "Command failed (#{command.join(' ')}): #{stderr.strip}"
  end

  def apply_github_update(dependency, dry_run:, latest_version:)
    source = dependency.source
    return if dry_run

    Dir.mktmpdir('ctxpm-github-') do |tmpdir|
      repo_dir = File.join(tmpdir, 'repo')
      command = ['git', 'clone', '--depth', '1']
      command += ['--branch', source['ref']] unless blank?(source['ref'])
      command += [source['url'], repo_dir]
      run_command(*command)

      source_path = File.join(repo_dir, source['path'])
      raise ScriptError, "Resolved GitHub source path #{source['path']} did not exist for #{dependency.name}." unless File.exist?(source_path)

      replace_canonical_path(source_path, dependency.absolute_path)
    end

    dependency.raw['version'] = latest_version
  end

  def apply_url_update(dependency, dry_run:, content:, latest_version:)
    return if dry_run

    target_path = canonical_target_for_url_dependency(dependency)
    FileUtils.mkdir_p(File.dirname(target_path))
    File.binwrite(target_path, content)
    dependency.raw['version'] = latest_version
  end

  def preserve_compatibility_paths(dependency, dry_run:)
    return if dry_run

    dependency.compatibility_paths.each do |compatibility_path|
      absolute_path = File.expand_path(compatibility_path, dependency.project_root)
      ensure_relative_symlink(absolute_path, dependency.absolute_path)
    end
  end

  def write_manifest(manifest_path, manifest)
    File.write(manifest_path, YAML.dump(manifest))
  end

  def apply_updates(manifest_path:, state_path:, names:, apply_all: false, dry_run: false)
    manifest = load_manifest(manifest_path)
    check_payload = build_check_payload(manifest_path: manifest_path, state_path: state_path, force: true, persist: false)
    results_by_name = Array(check_payload['dependencies']).each_with_object({}) do |result, acc|
      acc[result['name']] = result
    end

    candidate_names = if apply_all
                        results_by_name.values.select { |result| result['status'] == 'update_available' }.map { |result| result['name'] }
                      else
                        names
                      end
    raise ScriptError, 'No dependencies were selected for update.' if candidate_names.empty?

    ordered_names = candidate_names.uniq.sort_by { |name| name == 'ctxpm' ? 1 : 0 }
    applied = []
    skipped = []

    ordered_names.each do |name|
      dependency = find_dependency(manifest, manifest_path, name)
      raise ScriptError, "Dependency #{name.inspect} was not found in #{manifest_path}." unless dependency

      result = results_by_name[name] || resolve_dependency(dependency)
      case result['status']
      when 'up_to_date'
        skipped << { 'name' => name, 'status' => 'up_to_date' }
      when 'update_available'
        case dependency.source_type
        when 'github'
          apply_github_update(dependency, dry_run: dry_run, latest_version: result['latest_version'])
        when 'url'
          content = open_text(dependency.source['url'])
          apply_url_update(dependency, dry_run: dry_run, content: content, latest_version: result['latest_version'])
        else
          raise ScriptError, "Dependency #{name.inspect} has unsupported source type #{dependency.source_type.inspect}."
        end
        preserve_compatibility_paths(dependency, dry_run: dry_run)
        applied << {
          'name' => name,
          'status' => dry_run ? 'would_update' : 'updated',
          'current_version' => result['current_version'],
          'latest_version' => result['latest_version']
        }
      else
        skipped << {
          'name' => name,
          'status' => result['status'],
          'reason' => result['reason']
        }
      end
    end

    unless dry_run
      write_manifest(manifest_path, manifest)
      build_check_payload(manifest_path: manifest_path, state_path: state_path, force: true, persist: true)
    end

    {
      'status' => dry_run ? 'dry_run' : 'applied',
      'manifest_path' => manifest_path,
      'state_path' => state_path,
      'applied' => applied,
      'skipped' => skipped
    }
  end

  def render_apply_payload(payload)
    lines = []
    lines << "Apply status: #{payload['status']}"

    Array(payload['applied']).each do |item|
      lines << "- #{item['name']} [#{item['status']}] #{item['current_version']} -> #{item['latest_version']}"
    end
    Array(payload['skipped']).each do |item|
      line = "- #{item['name']} [#{item['status']}]"
      line += " reason=#{item['reason']}" if item['reason']
      lines << line
    end

    lines.join("\n")
  end
end
