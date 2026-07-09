void	ft_putstr_non_printable(char *str);

int	main(int argc, char **argv)
{
	if (argc < 2)
		return (1);
	ft_putstr_non_printable(argv[1]);
	return (0);
}
